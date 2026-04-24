package orchestrator

import (
	"context"
	"sync"
)

// WorkFunc is the per-task body the pool runs. It must respect ctx cancellation.
type WorkFunc func(ctx context.Context, id TaskID) TaskResult

type Pool struct {
	n     int
	sched *Scheduler
	work  WorkFunc
}

func NewPool(n int, sched *Scheduler, work WorkFunc) *Pool {
	if n < 1 {
		n = 1
	}
	return &Pool{n: n, sched: sched, work: work}
}

func (p *Pool) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for i := 0; i < p.n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case <-p.sched.Wait():
					return
				case id := <-p.sched.Ready():
					res := p.work(ctx, id)
					p.sched.Done(res)
				}
			}
		}()
	}
	wg.Wait()
	return ctx.Err()
}
