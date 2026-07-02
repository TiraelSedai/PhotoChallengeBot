package publish

import (
	"context"
	"fmt"
	"time"
)

type Stage struct {
	Claim   func(ctx context.Context, id int64, claimedAt time.Time) (bool, error)
	Release func(ctx context.Context, id int64, claimedAt time.Time) error
}

type Config struct {
	PersistTimeout time.Duration
}

type Lease struct {
	ClaimedAt time.Time

	stage          Stage
	id             int64
	persistTimeout time.Duration
}

func Attempt(
	ctx context.Context,
	cfg Config,
	stage Stage,
	id int64,
	claimedAt time.Time,
	body func(ctx context.Context, l *Lease) error,
) (bool, error) {
	if stage.Claim == nil {
		panic("publish: stage.Claim is nil")
	}
	if stage.Release == nil {
		panic("publish: stage.Release is nil")
	}
	if body == nil {
		panic("publish: body is nil")
	}

	ok, err := stage.Claim(ctx, id, claimedAt)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	lease := &Lease{
		ClaimedAt:      claimedAt,
		stage:          stage,
		id:             id,
		persistTimeout: cfg.PersistTimeout,
	}
	return true, body(ctx, lease)
}

func (l *Lease) persistContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), l.persistTimeout)
	}
	return context.WithTimeout(context.WithoutCancel(ctx), l.persistTimeout)
}

func (l *Lease) Release(ctx context.Context) error {
	pctx, cancel := l.persistContext(ctx)
	defer cancel()
	return l.stage.Release(pctx, l.id, l.ClaimedAt)
}

func (l *Lease) Commit(ctx context.Context, name string, mark func(pctx context.Context) (bool, error)) error {
	pctx, cancel := l.persistContext(ctx)
	defer cancel()
	ok, err := mark(pctx)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s: claim no longer owned", name)
	}
	return nil
}

func (l *Lease) CommitOrRecord(
	ctx context.Context,
	setName, recordName string,
	set, record func(pctx context.Context) (bool, error),
) error {
	pctx, cancel := l.persistContext(ctx)
	ok, err := set(pctx)
	cancel()
	if err == nil {
		if !ok {
			return fmt.Errorf("%s: claim no longer owned", setName)
		}
		return nil
	}

	rctx, rcancel := l.persistContext(ctx)
	rok, rerr := record(rctx)
	rcancel()
	if rerr != nil {
		return fmt.Errorf("%w; record %s: %v", err, recordName, rerr)
	}
	if !rok {
		return fmt.Errorf("%w; record %s: row not changed", err, recordName)
	}
	return nil
}
