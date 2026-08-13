package cli

import (
	"fmt"

	"github.com/bnkrr/rses/internal/daemon"
	"github.com/bnkrr/rses/internal/session"
)

type streamSummary struct {
	Results  map[string]string
	Statuses []session.Status
}

func streamOperation(req daemon.Request, handle func(daemon.OperationEvent)) (streamSummary, error) {
	var summary streamSummary
	var commandErr error
	err := daemon.Stream(req, func(event daemon.OperationEvent) error {
		if event.Type == "done" {
			summary.Results = event.Results
			summary.Statuses = event.Statuses
			return nil
		}
		if event.Type == "failed" && event.Name == "" && event.Error != "" {
			commandErr = fmt.Errorf("%s", event.Error)
		}
		if handle != nil {
			handle(event)
		}
		return nil
	})
	if err != nil {
		return summary, err
	}
	return summary, commandErr
}

func collectStatuses(req daemon.Request) ([]session.Status, error) {
	summary, err := streamOperation(req, nil)
	if err != nil {
		return nil, err
	}
	return summary.Statuses, nil
}

func printOperationEvent(event daemon.OperationEvent) {
	switch event.Type {
	case "progress":
		if event.Message != "" {
			fmt.Println(event.Message)
		}
	case "success", "skipped", "failed":
		if event.Name == "" {
			return
		}
		if event.Message != "" {
			fmt.Printf("%s: %s\n", event.Name, event.Message)
			return
		}
		if event.Error != "" {
			fmt.Printf("%s: %s\n", event.Name, event.Error)
		}
	}
}
