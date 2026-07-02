package publish

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func okStage() Stage {
	return Stage{
		Claim: func(ctx context.Context, id int64, claimedAt time.Time) (bool, error) {
			return true, nil
		},
		Release: func(ctx context.Context, id int64, claimedAt time.Time) error {
			return nil
		},
	}
}

func TestAttemptNotClaimed(t *testing.T) {
	stage := okStage()
	stage.Claim = func(ctx context.Context, id int64, claimedAt time.Time) (bool, error) {
		return false, nil
	}
	bodyRun := false
	ok, err := Attempt(context.Background(), Config{}, stage, 7, time.Now(),
		func(ctx context.Context, l *Lease) error {
			bodyRun = true
			return nil
		})
	if ok {
		t.Fatalf("expected ok=false")
	}
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if bodyRun {
		t.Fatalf("body must not run when claim not owned")
	}
}

func TestAttemptClaimError(t *testing.T) {
	sentinel := errors.New("claim failed")
	stage := okStage()
	stage.Claim = func(ctx context.Context, id int64, claimedAt time.Time) (bool, error) {
		return false, sentinel
	}
	bodyRun := false
	ok, err := Attempt(context.Background(), Config{}, stage, 7, time.Now(),
		func(ctx context.Context, l *Lease) error {
			bodyRun = true
			return nil
		})
	if ok {
		t.Fatalf("expected ok=false")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel err, got %v", err)
	}
	if bodyRun {
		t.Fatalf("body must not run on claim error")
	}
}

func TestAttemptBodyErrorPropagated(t *testing.T) {
	sentinel := errors.New("body failed")
	ok, err := Attempt(context.Background(), Config{}, okStage(), 7, time.Now(),
		func(ctx context.Context, l *Lease) error {
			return sentinel
		})
	if !ok {
		t.Fatalf("expected ok=true when claimed")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected body err, got %v", err)
	}
}

func TestAttemptNoAutoRelease(t *testing.T) {
	released := false
	stage := okStage()
	stage.Release = func(ctx context.Context, id int64, claimedAt time.Time) error {
		released = true
		return nil
	}
	_, err := Attempt(context.Background(), Config{PersistTimeout: time.Second}, stage, 7, time.Now(),
		func(ctx context.Context, l *Lease) error {
			return nil
		})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if released {
		t.Fatalf("runner must never auto-release")
	}
}

func TestReleaseUnderCancelledParent(t *testing.T) {
	reached := false
	stage := okStage()
	stage.Release = func(ctx context.Context, id int64, claimedAt time.Time) error {
		reached = true
		return ctx.Err()
	}

	parent, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Attempt(parent, Config{PersistTimeout: time.Second}, stage, 42, time.Now(),
		func(ctx context.Context, l *Lease) error {
			return l.Release(ctx)
		})
	if err != nil {
		t.Fatalf("release must succeed despite cancelled parent (persist ctx must drop parent cancellation), got %v", err)
	}
	if !reached {
		t.Fatalf("stage.Release was never reached")
	}
}

func TestReleaseWithNilCtx(t *testing.T) {
	var releaseCtx context.Context
	stage := okStage()
	stage.Release = func(ctx context.Context, id int64, claimedAt time.Time) error {
		releaseCtx = ctx
		return nil
	}
	_, err := Attempt(context.Background(), Config{PersistTimeout: time.Second}, stage, 1, time.Now(),
		func(ctx context.Context, l *Lease) error {
			return l.Release(nil)
		})
	if err != nil {
		t.Fatalf("release with nil ctx must work, got %v", err)
	}
	if releaseCtx == nil {
		t.Fatalf("stage.Release was never reached")
	}
	if _, hasDeadline := releaseCtx.Deadline(); !hasDeadline {
		t.Fatalf("nil-ctx release should still carry the persist timeout deadline")
	}
}

func leaseForTest(t *testing.T) *Lease {
	t.Helper()
	var l *Lease
	_, err := Attempt(context.Background(), Config{PersistTimeout: time.Second}, okStage(), 5, time.Now(),
		func(ctx context.Context, got *Lease) error {
			l = got
			return nil
		})
	if err != nil {
		t.Fatalf("attempt failed: %v", err)
	}
	return l
}

func TestCommitOK(t *testing.T) {
	l := leaseForTest(t)
	err := l.Commit(context.Background(), "mark reminder sent for challenge 5",
		func(pctx context.Context) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCommitNotOwned(t *testing.T) {
	l := leaseForTest(t)
	err := l.Commit(context.Background(), "mark reminder sent for challenge 5",
		func(pctx context.Context) (bool, error) { return false, nil })
	if err == nil || err.Error() != "mark reminder sent for challenge 5: claim no longer owned" {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestCommitError(t *testing.T) {
	l := leaseForTest(t)
	sentinel := errors.New("db down")
	err := l.Commit(context.Background(), "mark reminder sent for challenge 5",
		func(pctx context.Context) (bool, error) { return false, sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
}

func TestCommitOrRecordSetOK(t *testing.T) {
	l := leaseForTest(t)
	recordRun := false
	err := l.CommitOrRecord(context.Background(),
		"set results message id for challenge 5", "sent results message id",
		func(pctx context.Context) (bool, error) { return true, nil },
		func(pctx context.Context) (bool, error) { recordRun = true; return true, nil })
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if recordRun {
		t.Fatalf("record must not run when set succeeds")
	}
}

func TestCommitOrRecordSetNotOwned(t *testing.T) {
	l := leaseForTest(t)
	err := l.CommitOrRecord(context.Background(),
		"set results message id for challenge 5", "sent results message id",
		func(pctx context.Context) (bool, error) { return false, nil },
		func(pctx context.Context) (bool, error) { return true, nil })
	if err == nil || err.Error() != "set results message id for challenge 5: claim no longer owned" {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestCommitOrRecordSetErrRecordOK(t *testing.T) {
	l := leaseForTest(t)
	setErr := errors.New("set failed")
	err := l.CommitOrRecord(context.Background(),
		"set results message id for challenge 5", "sent results message id",
		func(pctx context.Context) (bool, error) { return false, setErr },
		func(pctx context.Context) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("record success should yield nil, got %v", err)
	}
}

func TestCommitOrRecordSetErrRecordErr(t *testing.T) {
	l := leaseForTest(t)
	setErr := errors.New("set failed")
	recErr := errors.New("record failed")
	err := l.CommitOrRecord(context.Background(),
		"set results message id for challenge 5", "sent results message id",
		func(pctx context.Context) (bool, error) { return false, setErr },
		func(pctx context.Context) (bool, error) { return false, recErr })
	if !errors.Is(err, setErr) {
		t.Fatalf("set err must be wrapped with %%w, got %v", err)
	}
	want := "set failed; record sent results message id: record failed"
	if err.Error() != want {
		t.Fatalf("unexpected err text: %q", err.Error())
	}
}

func TestCommitOrRecordSetErrRecordNotChanged(t *testing.T) {
	l := leaseForTest(t)
	setErr := errors.New("set failed")
	err := l.CommitOrRecord(context.Background(),
		"set results message id for challenge 5", "sent results message id",
		func(pctx context.Context) (bool, error) { return false, setErr },
		func(pctx context.Context) (bool, error) { return false, nil })
	if !errors.Is(err, setErr) {
		t.Fatalf("set err must be wrapped with %%w, got %v", err)
	}
	want := "set failed; record sent results message id: row not changed"
	if err.Error() != want {
		t.Fatalf("unexpected err text: %q", err.Error())
	}
}

func TestAttemptPanicsOnNilClaim(t *testing.T) {
	defer expectPanic(t)
	stage := okStage()
	stage.Claim = nil
	Attempt(context.Background(), Config{}, stage, 1, time.Now(),
		func(ctx context.Context, l *Lease) error { return nil })
}

func TestAttemptPanicsOnNilRelease(t *testing.T) {
	defer expectPanic(t)
	stage := okStage()
	stage.Release = nil
	Attempt(context.Background(), Config{}, stage, 1, time.Now(),
		func(ctx context.Context, l *Lease) error { return nil })
}

func TestAttemptPanicsOnNilBody(t *testing.T) {
	defer expectPanic(t)
	Attempt(context.Background(), Config{}, okStage(), 1, time.Now(), nil)
}

func expectPanic(t *testing.T) {
	t.Helper()
	r := recover()
	if r == nil {
		t.Fatalf("expected panic")
	}
	if msg, ok := r.(string); ok && !strings.HasPrefix(msg, "publish:") {
		t.Fatalf("unexpected panic message: %q", msg)
	}
}
