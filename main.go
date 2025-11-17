package main

import (
	"fmt"
	"log"
	"github.com/spf13/pflag"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/pelletier/go-toml/v2"
)

// main() calls parseSubjects which reads subjects.toml, loops over subjects to call getSubject
// getSubject gets the sources of each subject and calls getSource.
// since sources can have multiple repos (e.g. "github.com/you/"), find the appropriate git API in api.go
// then get the repos of that user from the API, and call getRepo for each
// getRepo does the actual soft-cloning and commit analysis
// main then directs control flow to output.go based on args

// some git APIs (github/gitlab?) support a /events endpoint; recent account activity
// i've had a lot of trouble getting these APIs to give me more than ~50 events. also they throttle
// so, just sticking to cloning for now. rearchitecting would probably give more useful data

type Source struct {
	url   string
	host  string
	user  string
	repos []*git.Repository
}

type Subject struct {
	Name    string
	Sources []Source
	// stuff these into a hashset/map so they're deduplicated in case the sources are redundant
	Commits map[plumbing.Hash]*object.Commit
}

const subjectsFile = "subjects.toml"
const savePath = "snapshots"

func parseSubjects() []Subject {
	data, err := os.ReadFile(subjectsFile)
	if err != nil {
		log.Fatalf("Failed to read %s: %v", subjectsFile, err)
	}

	var raw map[string]struct {
		Sources []string `toml:"sources"`
	}
	if err := toml.Unmarshal(data, &raw); err != nil {
		log.Fatalf("Failed to unmarshal TOML: %v", err)
	}

	var subjects []Subject
	for name, entry := range raw {
		subject := getSubject(name, entry.Sources)
		subjects = append(subjects, subject)
	}
	return subjects
}

func getSubject(name string, sourceURLs []string) Subject {
	log.Printf("--- Building Subject: %s ---\n", name)
	subject := Subject{
		Name:    name,
		Commits: make(map[plumbing.Hash]*object.Commit),
	}
	
	for _, sourceURL := range sourceURLs {
		source, commits := getSource(sourceURL, name)
		if source == nil {
			continue
		}
		subject.Sources = append(subject.Sources, *source)
		
		for _, commit := range commits {
			subject.Commits[commit.Hash] = commit
		}
	}
	
	log.Printf("Total unique commits for %s: %d\n", name, len(subject.Commits))
	return subject
}

func getSource(rawURL string, subjectName string) (*Source, []*object.Commit) {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}
	
	parsed, err := url.Parse(rawURL)
	if err != nil {
		log.Printf("Failed to parse URL %s: %v", rawURL, err)
		return nil, nil
	}

	host := parsed.Hostname()
	path := strings.Trim(parsed.Path, "/")
	
	if path == "" {
		log.Printf("URL has no path: %s", rawURL)
		return nil, nil
	}
	
	parts := strings.Split(path, "/")
	user := parts[0]
	var repoName string
	if len(parts) > 1 {
		repoName = parts[1]
	}
	
	source := &Source{
		url:  rawURL,
		host: host,
		user: user,
	}

	// if source is a repo and not a git user, we can just clone it.
	// if it isn't, we have to call detectAPI to try to determine how to enumerate a user's repos
	var repoURLs []string
	if repoName != "" {
		cloneURL := fmt.Sprintf("https://%s/%s/%s.git", host, user, repoName)
		repoURLs = []string{cloneURL}
	} else {
		fetcher := detectAPI(host)
		if fetcher == nil {
			log.Printf("Unknown API for host %s", host)
			return nil, nil
		}
		// a corresponding fetcher for each git host API
		repoURLs, err = fetcher(host, user, flags)
		if err != nil {
			log.Printf("Failed to fetch repos for %s on host %s: %v", user, host, err)
			return nil, nil
		}
	}

	log.Printf("Processing source: %s (%d repos)\n", rawURL, len(repoURLs))
	
	var allCommits []*object.Commit
	for _, repoURL := range repoURLs {
		repo, commits := getRepo(repoURL, subjectName, user)
		if repo != nil {
			source.repos = append(source.repos, repo)
			allCommits = append(allCommits, commits...)
		}
	}
	return source, allCommits
}

func getRepo(repoURL string, subjectName string, sourceUser string) (*git.Repository, []*object.Commit) {
	repo, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL:        repoURL,
		Filter:     packp.FilterBlobNone(),
		NoCheckout: true,
	})
	if err != nil {
		log.Printf("  Failed to clone repository %s: %v", repoURL, err)
		return nil, nil
	}
	
	head, err := repo.Head()
	if err != nil {
		log.Printf("  Failed to get HEAD for %s: %v", repoURL, err)
		return nil, nil
	}

	commitIter, err := repo.Log(&git.LogOptions{From: head.Hash()})
	if err != nil {
		log.Printf("  Failed to get commit log for %s: %v", repoURL, err)
		return nil, nil
	}

	var commits []*object.Commit
	err = commitIter.ForEach(func(c *object.Commit) error {
		if validateCommit(c, subjectName, sourceUser) {
			commits = append(commits, c)
		}
		return nil
	})

	if err != nil {
		log.Printf("  Failed to iterate commits for %s: %v", repoURL, err)
		return nil, nil
	}

	log.Printf("  Found %d commits in repo %s\n", len(commits), repoURL)
	return repo, commits
}

// i am already filtering old repos (last-pushed-at) via APIs, but not old commits
// anything older than 1 month gets thrown out
func validateCommit(commit *object.Commit, subjectName string, githubUsername string) bool {

	if !commit.Committer.When.After(flags.Since) {
		return false
	}

	// TODO: slop ahead
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

func buildSubjectFromFlag(userFlag string) Subject {
	parts := strings.Split(userFlag, "@")
	if len(parts) != 2 {
		log.Fatalf("Invalid format, expected: name@url1,url2")
	}
	
	name := parts[0]
	urls := strings.Split(parts[1], ",")
	
	return getSubject(name, urls)
}

type Flags struct {
	User			string
	Since			time.Time
	Write			bool
	StdOut		bool
	PlotScatter bool
	PlotHisto	bool
} 
var flags Flags

func main() {
	pflag.StringVarP(&flags.User, "user", "u", "", "manually supply e.g. user@source1,source2,source3")
	var age int
	pflag.IntVarP(&age, "since", "s", 90, "how many days ago to begin tracking (default 90)")
	pflag.BoolVarP(&flags.Write, "write", "w", true, "write snapshot to disk")
	pflag.BoolVarP(&flags.StdOut, "stdout", "o", true, "output sleep schedule estimate")
	pflag.BoolVarP(&flags.PlotScatter, "plot-scatter", "p", false, "generate scatter plot")
	pflag.BoolVarP(&flags.PlotHisto, "plot-histo", "h", false, "generate histogram")
	pflag.Parse()
	flags.Since = time.Now().AddDate(0, 0, -age)

	var subjects []Subject
	if flags.User != "" {
		subject := buildSubjectFromFlag(flags.User)
		subjects = []Subject{subject}
	} else {
		subjects = parseSubjects()
		if len(subjects) == 0 {
			log.Fatal("No subjects found")
		}
	}
	output(subjects, flags)
}

