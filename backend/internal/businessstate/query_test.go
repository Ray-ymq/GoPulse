package businessstate

import (
	"context"
	"testing"
	"time"
)

type stageLogReaderStub struct {
	query LogCorrelationQuery
}

func (reader *stageLogReaderStub) QueryCorrelation(_ context.Context, query LogCorrelationQuery) (LogCorrelationResult, error) {
	reader.query = query
	return LogCorrelationResult{Quality: StageQualityNoEvidence}, nil
}

func TestReadLogsForwardsExactlyOneCallerSelector(t *testing.T) {
	const epoch = "123e4567-e89b-42d3-a456-426614174000"
	const commandID = "123e4567-e89b-42d3-a456-426614174001"
	const eventID = "123e4567-e89b-42d3-a456-426614174002"
	for name, query := range map[string]StageQuery{
		"command": {DataEpoch: epoch, CommandID: commandID, EventID: eventID, PostID: 42, LogSources: []string{StageSourceLogs}},
		"event":   {DataEpoch: epoch, CommandID: commandID, EventID: eventID, PostID: 42, LogSources: []string{StageSourceLogs}},
		"post":    {DataEpoch: epoch, CommandID: commandID, EventID: eventID, PostID: 42, LogSources: []string{StageSourceLogs}},
	} {
		reader := &stageLogReaderStub{}
		service := &QueryService{logs: reader}
		response := &StageResponse{Resolved: StageResolved{CommandID: commandID, EventID: eventID, PostID: 42}}
		switch name {
		case "command":
			query.EventID, query.PostID = "", 0
		case "event":
			query.CommandID, query.PostID = "", 0
		case "post":
			query.CommandID, query.EventID = "", ""
		}

		service.readLogs(context.Background(), query, persistentStage{}, response, time.Now().UTC())
		selectors := 0
		selectors += boolToInt(reader.query.CommandID != "")
		selectors += boolToInt(reader.query.EventID != "")
		selectors += boolToInt(reader.query.PostID != 0)
		if selectors != 1 {
			t.Fatalf("%s forwarded %d selectors: %#v", name, selectors, reader.query)
		}
		if name == "command" && reader.query.CommandID != commandID || name == "event" && reader.query.EventID != eventID || name == "post" && reader.query.PostID != 42 {
			t.Fatalf("%s forwarded the wrong selector: %#v", name, reader.query)
		}
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
