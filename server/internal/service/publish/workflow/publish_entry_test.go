package workflow

import "testing"

func TestExecutePublishCannotBypassDownloaderGuard(t *testing.T) {
	previous := checkPublishDownloaderGate
	calledWith := ""
	checkPublishDownloaderGate = func(downloaderID string) (bool, string) {
		calledWith = downloaderID
		return false, "达到数量限制"
	}
	t.Cleanup(func() { checkPublishDownloaderGate = previous })

	result, status := ExecutePublish(PublishExecutionInput{
		TargetSite: "target",
		TargetInfo: map[string]any{"nickname": "Target"},
		UploadData: map[string]any{"title": "Example"},
		Payload: map[string]any{
			"savePath":     "/downloads",
			"downloaderId": "qb-main",
		},
	}, PublishExecutionDeps{})

	if status != 200 || result["limit_reached"] != true {
		t.Fatalf("unexpected result: status=%d result=%#v", status, result)
	}
	if calledWith != "qb-main" {
		t.Fatalf("guard called with %q", calledWith)
	}
}
