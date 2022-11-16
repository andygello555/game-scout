package steamcmd

import (
	"bufio"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func ExampleSteamCMD_Flow() {
	var err error
	cmd := New(true)

	if err = cmd.Flow(
		NewCommandWithArgs(AppInfoPrint, 477160),
		NewCommandWithArgs(Quit),
	); err != nil {
		fmt.Printf("Could not execute flow: %s\n", err.Error())
	}
	fmt.Println(cmd.ParsedOutputs[0].(map[string]any)["common"].(map[string]any)["name"])
	// Output:
	// Human: Fall Flat
}

type steamCMDFlowJob struct {
	appID int
	jobID int
}

type steamCMDFlowResult struct {
	jobID        int
	appID        int
	parsedOutput any
	err          error
}

func steamCMDFlowWorker(wg *sync.WaitGroup, jobs <-chan *steamCMDFlowJob, results chan<- *steamCMDFlowResult) {
	defer wg.Done()
	for job := range jobs {
		var err error
		cmd := New(true)
		err = cmd.Flow(
			NewCommandWithArgs(AppInfoPrint, job.appID),
			NewCommandWithArgs(Quit),
		)
		results <- &steamCMDFlowResult{
			jobID:        job.jobID,
			appID:        job.appID,
			parsedOutput: cmd.ParsedOutputs[0],
			err:          err,
		}
	}
}

const (
	sampleGameWebsitesPath = "../samples/sampleGameWebsites.txt"
)

func benchmarkSteamCMDFlow(workers int, b *testing.B) {
	var err error
	s := rand.NewSource(time.Now().Unix())
	r := rand.New(s)

	// First we load the appIDs from the sample game websites into an array
	sampleAppIDs := make([]int, 0)
	var file *os.File
	if file, err = os.Open(sampleGameWebsitesPath); err != nil {
		b.Fatalf("Cannot open %s: %s", sampleGameWebsitesPath, err.Error())
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		// Parse the scanned text to a URL
		text := scanner.Text()
		var u *url.URL
		if u, err = url.Parse(strings.TrimSpace(text)); err != nil {
			b.Fatalf("Could not parse \"%s\" to a URL", text)
		}

		// Find the appID in the path of the URL (aka. the integer value)
		var appID int64
		for _, path := range strings.Split(u.Path, "/") {
			if appID, err = strconv.ParseInt(path, 10, 64); err == nil {
				break
			}
		}

		if appID != 0 {
			sampleAppIDs = append(sampleAppIDs, int(appID))
		} else {
			b.Fatalf("Could not find appID in \"%s\"", u.Path)
		}
	}

	if err = file.Close(); err != nil {
		b.Fatalf("Could not open %s: %s", sampleGameWebsitesPath, err.Error())
	}

	// Then we start our workers
	jobs := make(chan *steamCMDFlowJob, b.N)
	results := make(chan *steamCMDFlowResult, b.N)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go steamCMDFlowWorker(&wg, jobs, results)
	}

	// We reset the timer as we have completed the setup of the benchmark then queue up all our jobs. We use a random
	// appID from the sampleAppIDs array.
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jobs <- &steamCMDFlowJob{
			appID: sampleAppIDs[r.Intn(len(sampleAppIDs))],
			jobID: i,
		}
	}

	// Close the channels and wait for the workers to finish.
	close(jobs)
	wg.Wait()
	close(results)

	// Finally, we read each result from the closed channel to see if we have any errors or parsed outputs that cannot
	// be asserted to a map.
	for result := range results {
		if _, ok := result.parsedOutput.(map[string]any); result.err != nil || !ok {
			b.Errorf(
				"Error occurred (%v)/parsed output could not be asserted to map (output: %v), in job no. %d (appID: %d)",
				result.err, result.parsedOutput, result.jobID, result.appID,
			)
		}
	}
}

func BenchmarkSteamCMD_Flow5(b *testing.B)  { benchmarkSteamCMDFlow(5, b) }
func BenchmarkSteamCMD_Flow10(b *testing.B) { benchmarkSteamCMDFlow(10, b) }
