package main

import (
	"fmt"
	"image/color"
	"time"
	"log"
	"strings"
	"path/filepath"
	"os"

	"github.com/pelletier/go-toml/v2"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
)

func output(subjects []Subject, flags Flags) {
	if len(subjects) == 0 {
		log.Fatal("No subjects found")
	}

	for _, subject := range subjects {
		if len(subject.Commits) == 0 {
			log.Printf("No commits found for %s. Skipping output.", subject.Name)
			continue
		}

		if flags.StdOut {
			if err := printSleepHisto(&subject); err != nil {
				log.Printf("Failed to print sleep histogram for %s: %v", subject.Name, err)
			}
		}
		if flags.PlotScatter {
			outputFilename := fmt.Sprintf("%s_commits_scatter.png", subject.Name)
			if err := plotCommitsScatter(&subject, outputFilename); err != nil {
				log.Printf("Failed to save scatter plot for %s: %v", subject.Name, err)
			} else {
				fmt.Printf("Saved scatter plot to %s\n", outputFilename)
			}
		}
		if flags.PlotHisto {
			outputFilename := fmt.Sprintf("%s_commits_histogram.png", subject.Name)
			if err := plotCommitsHistogram(&subject, outputFilename); err != nil {
				log.Printf("Failed to save histogram for %s: %v", subject.Name, err)
			} else {
				fmt.Printf("Saved histogram to %s\n", outputFilename)
			}
		}
	}
}

func printSleepHisto(subject *Subject) error {
	var maxi int
	hourCounts := make([]int, 24)
	for _, c := range subject.Commits {
		t := c.Author.When
		hour := t.Hour()
		hourCounts[hour]++
		if hour > maxi {
			maxi = hour
		}
	}

	// 0-pad according to the # of digits in max value
	width := len(fmt.Sprintf("%d", maxi))

	log.Printf("Sleep histogram for user %s:\n", subject.Name)

	// assumed terminal width of 80
	if maxi > 80 {
		scalingFactor := float64(80) / float64(maxi)
		for hour, count := range hourCounts {
			hashtags := strings.Repeat("#", int(float64(count) * scalingFactor))
			fmt.Printf("%02d:00 (%0*d): %s\n", hour, width, count, hashtags)
		}
	} else {
		for hour, count := range hourCounts {
			hashtags := strings.Repeat("#", count)
			fmt.Printf("%02d:00 (%0*d): %s\n", hour, width, count, hashtags)
		}
	}

	if flags.Write {
		save(subject, hourCounts)
	}

	return nil
}

// TODO: slop
// plotCommitsScatter creates a scatter plot of commit timestamps
func plotCommitsScatter(subject *Subject, outputPath string) error {
	// Convert commits map to plotter points
	pts := make(plotter.XYs, 0, len(subject.Commits))
	for _, c := range subject.Commits {
		t := c.Author.When
		secondsSinceMidnight := t.Hour()*3600 + t.Minute()*60 + t.Second()
		pts = append(pts, plotter.XY{
			X: float64(t.Unix()),
			Y: float64(secondsSinceMidnight),
		})
	}

	green := color.RGBA{0x95, 0xd5, 0x50, 0xff}
	p := plot.New()
	p.BackgroundColor = color.RGBA{0x10, 0x10, 0x10, 0xff}
	p.Title.Text = fmt.Sprintf("Commit Schedule: %s (Scatter)", subject.Name)
	p.Title.TextStyle.Color = green
	p.X.Label.Text = "Commit Date"
	p.X.Label.TextStyle.Color = green
	p.X.Color = green
	p.X.Tick.Color = green
	p.X.Tick.Label.Color = green
	p.X.Tick.Marker = dateTicks{}
	p.Y.Label.Text = "Time of Day"
	p.Y.Label.TextStyle.Color = green
	p.Y.Color = green
	p.Y.Tick.Color = green
	p.Y.Tick.Label.Color = green
	p.Y.Tick.Marker = hourTicks{}
	
	scatter, err := plotter.NewScatter(pts)
	if err != nil {
		return fmt.Errorf("could not create scatter plot: %v", err)
	}
	scatter.Radius = vg.Points(2)
	scatter.Color = green
	p.Add(scatter)
	
	if err := p.Save(10*vg.Inch, 6*vg.Inch, outputPath); err != nil {
		return fmt.Errorf("could not save plot: %v", err)
	}
	return nil
}

// TODO: slop
// plotCommitsHistogram creates a histogram of commits by hour of day
func plotCommitsHistogram(subject *Subject, outputPath string) error {
	// Count commits per hour
	hourCounts := make([]float64, 24)
	for _, c := range subject.Commits {
		t := c.Author.When
		hour := t.Hour()
		hourCounts[hour]++
	}

	// Create bar chart values
	values := make(plotter.Values, 24)
	for i  := range 24 {
		values[i] = hourCounts[i]
	}

	green := color.RGBA{0x95, 0xd5, 0x50, 0xff}
	p := plot.New()
	p.BackgroundColor = color.RGBA{0x10, 0x10, 0x10, 0xff}
	p.Title.Text = fmt.Sprintf("Commit Distribution: %s (by Hour)", subject.Name)
	p.Title.TextStyle.Color = green
	p.X.Label.Text = "Hour of Day"
	p.X.Label.TextStyle.Color = green
	p.X.Color = green
	p.X.Tick.Color = green
	p.X.Tick.Label.Color = green
	p.Y.Label.Text = "Number of Commits"
	p.Y.Label.TextStyle.Color = green
	p.Y.Color = green
	p.Y.Tick.Color = green
	p.Y.Tick.Label.Color = green

	bars, err := plotter.NewBarChart(values, vg.Points(20))
	if err != nil {
		return fmt.Errorf("could not create bar chart: %v", err)
	}
	bars.Color = green
	bars.LineStyle.Color = green
	p.Add(bars)

	// Custom X-axis labels for hours
	p.NominalX(
		"00", "01", "02", "03", "04", "05", 
		"06", "07", "08", "09", "10", "11",
		"12", "13", "14", "15", "16", "17",
		"18", "19", "20", "21", "22", "23",
	)

	if err := p.Save(10*vg.Inch, 6*vg.Inch, outputPath); err != nil {
		return fmt.Errorf("could not save plot: %v", err)
	}
	return nil
}

// hourTicks provides formatted time-of-day labels for plot Y-axis
type hourTicks struct{}

func (hourTicks) Ticks(min, max float64) []plot.Tick {
	var ticks []plot.Tick
	for h := 0; h <= 24; h += 3 {
		seconds := float64(h * 3600)
		ticks = append(ticks, plot.Tick{
			Value: seconds,
			Label: fmt.Sprintf("%02d:00", h),
		})
	}
	return ticks
}

// dateTicks provides formatted date labels for plot X-axis
type dateTicks struct{}

func (dateTicks) Ticks(min, max float64) []plot.Tick {
	var ticks []plot.Tick
	minTime := time.Unix(int64(min), 0)
	maxTime := time.Unix(int64(max), 0)
	duration := max - min
	
	ticks = append(ticks, plot.Tick{Value: min, Label: minTime.Format("2006-01-02")})
	for i := 1; i <= 5; i++ {
		tickVal := min + (duration * float64(i) / 6.0)
		tickTime := time.Unix(int64(tickVal), 0)
		ticks = append(ticks, plot.Tick{Value: tickVal, Label: tickTime.Format("2006-01-02")})
	}
	ticks = append(ticks, plot.Tick{Value: max, Label: maxTime.Format("2006-01-02")})
	return ticks
}

func save(subject *Subject, times []int) {
	stamp := time.Now().UTC().Format("2006-01-02") + ".toml"
	path := filepath.Join(savePath, stamp)

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		log.Fatalf("could not make dir(s) %s: %v", filepath.Dir(path), err)
	}

	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("could not write file %s: %v", path, err)
	}
	defer f.Close()

	mappedTimes := map[string][]int{
		subject.Name: times,
	}

	if err := toml.NewEncoder(f).Encode(mappedTimes); err != nil {
		log.Fatalf("encode %s: %v", path, err)
	}	
}

// maybe
// func serialize(subject *Subject) {
// 	stamp := time.Now().UTC().Format("2006-01-02_15-04-05")
// 	path := filepath.Join(savePath, subject.Name, stamp)
//
// 	fs := osfs.New(path)
// 	store := filesystem.NewStorage(fs)
//
// 	for _, commit := range subject.Commits {
// 		
// 	}
// }

// find likely sleep windows
// too many assumptions i think
// func estimateSleepSchedule(subject *Subject) {
// 	if len(subject.Commits) == 0 {
// 		fmt.Printf("No commits to analyze for %s\n", subject.Name)
// 		return
// 	}
//
// 	// build histogram of activity by hour
// 	hourCounts := make([]int, 24)
// 	for _, c := range subject.Commits {
// 		t := c.Author.When
// 		hour := t.Hour()
// 		hourCounts[hour]++
// 	}
//
// 	// Find the longest consecutive sequence of low-activity hours
// 	// Low activity = fewer than 5% of average hourly commits
// 	totalCommits := len(subject.Commits)
// 	avgPerHour := float64(totalCommits) / 24.0
// 	threshold := int(avgPerHour * 0.05)
// 	if threshold < 1 {
// 		threshold = 1
// 	}
//
// 	var longestStart, longestLen int
// 	currentStart, currentLen := -1, 0
//
// 	for i := 0; i < 48; i++ { // Check twice around the clock to handle wrap-around
// 		hour := i % 24
// 		if hourCounts[hour] <= threshold {
// 			if currentLen == 0 {
// 				currentStart = hour
// 			}
// 			currentLen++
// 			if currentLen > longestLen {
// 				longestLen = currentLen
// 				longestStart = currentStart
// 			}
// 		} else {
// 			currentLen = 0
// 		}
// 	}
//
// 	if longestLen >= 4 { // At least 4 hours of inactivity
// 		sleepStart := longestStart
// 		sleepEnd := (longestStart + longestLen) % 24
// 		
// 		fmt.Printf("\n=== Sleep Schedule Estimate for %s ===\n", subject.Name)
// 		fmt.Printf("Estimated sleep window: %02d:00 - %02d:00\n", sleepStart, sleepEnd)
// 		fmt.Printf("Duration: ~%d hours\n", longestLen)
// 		fmt.Printf("Based on %d commits\n", totalCommits)
// 		fmt.Printf("Low-activity threshold: ≤%d commits/hour\n\n", threshold)
// 	} else {
// 		fmt.Printf("\n=== Sleep Schedule Estimate for %s ===\n", subject.Name)
// 		fmt.Printf("Unable to identify clear sleep window (no extended low-activity period)\n")
// 		fmt.Printf("This may indicate irregular sleep patterns or insufficient data\n\n")
// 	}
// }

