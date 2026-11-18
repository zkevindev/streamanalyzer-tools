package diagnostic

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

type testExplainer struct {
	identity ModelIdentity
	explain  func(context.Context) (string, error)
}

func (m testExplainer) Identity() ModelIdentity { return m.identity }

func (m testExplainer) Explain(ctx context.Context, _ Report) (Explanation, error) {
	summary, err := m.explain(ctx)
	return Explanation{Provider: m.identity.Provider, Model: m.identity.Model, Summary: summary}, err
}

func openRunForExplanation(t *testing.T, model Explainer) (*Service, string) {
	t.Helper()
	service, err := Open(t.TempDir(), model)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	run := newRun("sample", ProtocolHTTPFLV, "")
	run.Status, run.ReportReady = "done", true
	dir := service.runDir(run.ID)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := persistReport(dir, Report{Manifest: Manifest{Protocol: run.Protocol}}); err != nil {
		t.Fatal(err)
	}
	if err := service.insertRun(run); err != nil {
		t.Fatal(err)
	}
	return service, run.ID
}

func TestModelSwitchCancelsAndDiscardsPreviousResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started := make(chan struct{})
	canceled := make(chan struct{})
	finishPrevious := make(chan struct{})
	previous := testExplainer{
		identity: ModelIdentity{Provider: ProviderOpenAICompatible, Model: "before"},
		explain: func(request context.Context) (string, error) {
			close(started)
			<-request.Done()
			close(canceled)
			select {
			case <-finishPrevious:
				return "stale", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}
	current := testExplainer{
		identity: ModelIdentity{Provider: ProviderDeepSeek, Model: "after"},
		explain:  func(context.Context) (string, error) { return "updated", nil },
	}
	service, id := openRunForExplanation(t, previous)
	previousError := make(chan error, 1)
	go func() {
		_, err := service.Explain(ctx, id)
		previousError <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("previous model did not receive the request")
	}
	if _, err := service.Explain(ctx, id); !errors.Is(err, ErrBusy) {
		t.Fatalf("an active model accepted another request: %v", err)
	}
	service.SetModel(current)
	select {
	case <-canceled:
	case <-ctx.Done():
		t.Fatal("previous model request was not canceled")
	}
	if got := service.ModelIdentity(); got != current.identity {
		t.Fatalf("active identity: %+v", got)
	}
	if got, err := service.Explain(ctx, id); err != nil || got.Summary != "updated" {
		t.Fatalf("new model did not explain immediately: %+v, %v", got, err)
	}
	close(finishPrevious)
	select {
	case err := <-previousError:
		if !errors.Is(err, ErrModelChanged) {
			t.Fatalf("stale request should report configuration change: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("previous model did not finish")
	}
	if got, err := service.Detail(id); err != nil || got.Explanation == nil || got.Explanation.Summary != "updated" {
		t.Fatalf("stale request replaced the new explanation: %+v, %v", got.Explanation, err)
	}
}

func TestRefreshExplanationUsesNewConfigurationForSameModel(t *testing.T) {
	identity := ModelIdentity{Provider: ProviderOpenAICompatible, Model: "same-model"}
	service, id := openRunForExplanation(t, testExplainer{identity: identity, explain: func(context.Context) (string, error) {
		return "original", nil
	}})
	if _, err := service.Explain(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	service.SetModel(testExplainer{identity: identity, explain: func(context.Context) (string, error) {
		return "", ErrProvider
	}})
	if _, err := service.RefreshExplanation(t.Context(), id); !errors.Is(err, ErrProvider) {
		t.Fatalf("refresh did not use the new configuration: %v", err)
	}
	if got, err := service.Explain(t.Context(), id); err != nil || got.Summary != "original" {
		t.Fatalf("failed refresh changed the cached explanation: %+v, %v", got, err)
	}
	service.SetModel(testExplainer{identity: identity, explain: func(context.Context) (string, error) {
		return "updated", nil
	}})
	if got, err := service.RefreshExplanation(t.Context(), id); err != nil || got.Summary != "updated" {
		t.Fatalf("refresh did not replace the previous explanation: %+v, %v", got, err)
	}
}
