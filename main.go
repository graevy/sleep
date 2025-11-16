package main

// overall structure:
// main() reads subjects.toml (getSubjects), looking to construct Subject structs
// each Subject has Sources, e.g. "github.com/user", a place where repo(s) need to get cloned
// getSubjects calls parseSource to get those repos. each common git host has its own API,
// and this code is abstracted into api.go 
// there's some hacky detectAPI() and an account->repos fetcher function for each API
// recent-only commit logic is delegated to those fetcher functions
// then, parseSource clones the actual repos 
// the `blob:none` git-clone filter only clones commit metadata because we only want timestamps
// then we must entity-resolve the user to check if they made the commits. this sucks and is bad
// now we have a giant list of recency-biased timestamps to do what with?
// -o dumps estimated start,end sleep schedule timestamps to stdout (default),
// -p prints a scatter plot to file,
// -h saves a histogram to file,
// TODO: -s serializes and dumps raw commit timestamps to `subjects/user/snapshot-<ISO8601>`

import (
	"fmt"
	"log"
	"flag"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/pelletier/go-toml/v2"
)


type Source struct {
	url   string
	host  string
	user  string
	repos []*git.Repository
}

type Subject struct {
	Name    string
	Sources []Source
}

type CommitTimestamp struct {
	Timestamp  time.Time
	TimeOfDay  int // seconds since midnight
}

const subjectsFile = "subjects.toml"

func getSubjects() ([]Subject, error) {
	data, err := os.ReadFile(subjectsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", subjectsFile, err)
	}

	var raw map[string]struct {
		Sources []string `toml:"sources"`
	}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal TOML: %w", err)
	}

	var subjects []Subject
	for name, entry := range raw {
		subject := Subject{Name: name}
		
		// Parse each source URL into a Source struct
		for _, sourceURL := range entry.Sources {
			source, err := parseSource(sourceURL)
			if err != nil {
				log.Printf("Failed to parse source %s for subject %s: %v", sourceURL, name, err)
				continue
			}
			subject.Sources = append(subject.Sources, *source)
		}
		subjects = append(subjects, subject)
	}
	return subjects, nil
}

// creates a Source object from a url string in a toml
func parseSource(raw string) (*Source, error) {
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}
	
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL %s: %w", raw, err)
	}

	host := parsed.Hostname()
	path := strings.Trim(parsed.Path, "/")
	
	if path == "" {
		return nil, fmt.Errorf("URL has no path: %s", raw)
	}
	
	parts := strings.Split(path, "/")
	user := parts[0]
	var repoName string
	if len(parts) >= 2 {
		repoName = parts[1]
	}
	
	res := &Source{
		url:  raw,
		host: host,
		user: user,
	}
	
	fetcher := detectAPI(host)
	if fetcher == nil {
		return nil, fmt.Errorf("unknown API for host %s", host)
	}
	
	var repoURLs []string
	if repoName != "" {
		cloneURL := fmt.Sprintf("https://%s/%s/%s.git", host, user, repoName)
		repoURLs = []string{cloneURL}
	} else {
		repoURLs, err = fetcher(host, user)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch repos for %s on host %s: %w", user, host, err)
		}
	}

	// cloning step
	for _, repoURL := range repoURLs {
		repo, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
			URL:        repoURL,
			Filter:     packp.FilterBlobNone(),
			NoCheckout: true,
		})
		if err != nil {
			log.Printf("Failed to clone repository %s: %v", repoURL, err)
			continue
		}
		res.repos = append(res.repos, repo)
	}
	return res, nil
}

// extracts commit time data from all repos in a Source
func parseRepos(source *Source, subjectName string) ([]CommitTimestamp, error) {
	var allTimestamps []CommitTimestamp

	fmt.Printf("Processing source: %s (%d repos)\n", source.url, len(source.repos))

	for _, repo := range source.repos {
		head, err := repo.Head()
		if err != nil {
			log.Printf("Failed to get HEAD: %v", err)
			continue
		}

		commitIter, err := repo.Log(&git.LogOptions{From: head.Hash()})
		if err != nil {
			log.Printf("Failed to get commit log: %v", err)
			continue
		}

		var commits []CommitTimestamp
		err = commitIter.ForEach(func(c *object.Commit) error {
			if !isCommitFromUser(c, subjectName, source.user) {
				return nil
			}

			t := c.Author.When
			secondsSinceMidnight := t.Hour()*3600 + t.Minute()*60 + t.Second()
			commits = append(commits, CommitTimestamp{
				Timestamp: t,
				TimeOfDay: secondsSinceMidnight,
			})
			return nil
		})

		if err != nil {
			log.Printf("Failed to iterate commits: %v", err)
			continue
		}

		fmt.Printf("Found %d commits in repo\n", len(commits))
		allTimestamps = append(allTimestamps, commits...)
	}
	return allTimestamps, nil
}

func isCommitFromUser(commit *object.Commit, subjectName string, githubUsername string) bool {
	authorName := strings.ToLower(commit.Author.Name)
	authorEmail := strings.ToLower(commit.Author.Email)
	
	if strings.Contains(authorName, strings.ToLower(subjectName)) {
		return true
	}
	
	if githubUsername != "" {
		username := strings.ToLower(githubUsername)
		
		if strings.Contains(authorName, username) {
			return true
		}
		if strings.Contains(authorEmail, username+"@users.noreply.github.com") {
			return true
		}
		if strings.HasPrefix(authorEmail, username+"@") {
			return true
		}
	}
	return false
}

// if someone passes `-u user@source1,source2`, quickly hack-together a Subject
func buildSubjectFromFlag(userFlag string) (Subject, error) {
	parts := strings.Split(userFlag, "@")
	if len(parts) != 2 {
		return Subject{}, fmt.Errorf("invalid format, expected: name@url1,url2")
	}
	
	name := parts[0]
	urls := strings.Split(parts[1], ",")
	
	subject := Subject{Name: name}
	for _, url := range urls {
		source, err := parseSource(url)
		if err != nil {
			log.Printf("Failed to parse source %s: %v", url, err)
			continue
		}
		subject.Sources = append(subject.Sources, *source)
	}
	return subject, nil
}

func main() {
	userFlag := flag.String("u", "", "manually supply e.g. user@source1,source2,source3")
	stdOut := flag.Bool("o", true, "output sleep schedule estimate")
	plotScatter := flag.Bool("p", false, "generate scatter plot")
	plotHisto := flag.Bool("h", false, "generate histogram")
	flag.Parse()

	var subjects []Subject
	var err error
	
	if *userFlag != "" {
		subject, err := buildSubjectFromFlag(*userFlag)
		if err != nil {
			log.Fatalf("Error parsing user flag: %v", err)
		}
		subjects = []Subject{subject}
	} else {
		subjects, err = getSubjects()
		if err != nil {
			log.Fatalf("Error reading subjects: %v", err)
		}
		if len(subjects) == 0 {
			log.Fatal("No subjects found")
		}
	}

	for _, subject := range subjects {
		fmt.Printf("--- Processing Subject: %s ---\n", subject.Name)

		if len(subject.Sources) == 0 {
			log.Printf("No sources found for %s. Skipping.", subject.Name)
			continue
		}

		var allTimestamps []CommitTimestamp
		for _, source := range subject.Sources {
			commits, err := parseRepos(&source, subject.Name) 
			if err != nil {
				log.Printf("Skipping source %s: %v", source.url, err)
				continue
			}
			allTimestamps = append(allTimestamps, commits...)
		}

		if len(allTimestamps) == 0 {
			log.Printf("No commits found for %s. Skipping.", subject.Name)
			continue
		}

		fmt.Printf("Total commits found for %s: %d\n", subject.Name, len(allTimestamps))

		if *stdOut {
			estimateSleepSchedule(allTimestamps, subject.Name)
		}
		if *plotScatter {
			outputFilename := fmt.Sprintf("%s_commits_scatter.png", subject.Name)
			if err := plotCommitsScatter(allTimestamps, outputFilename); err != nil {
				log.Printf("Failed to save scatter plot for %s: %v", subject.Name, err)
			} else {
				fmt.Printf("Saved scatter plot to %s\n", outputFilename)
			}
		}
		if *plotHisto {
			outputFilename := fmt.Sprintf("%s_commits_histogram.png", subject.Name)
			if err := plotCommitsHistogram(allTimestamps, outputFilename); err != nil {
				log.Printf("Failed to save histogram for %s: %v", subject.Name, err)
			} else {
				fmt.Printf("Saved histogram to %s\n", outputFilename)
			}
		}
		fmt.Println("--------------------------------")
	}
}

