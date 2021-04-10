package client

import "github.com/rs/zerolog/log"

// NewDispatcher creates, and returns a new Dispatcher object.
func NewDispatcher(jobQueue <-chan ServerTransferJob, maxWorkers int) *Dispatcher {
	workerPool := make(chan chan ServerTransferJob, maxWorkers)

	return &Dispatcher{
		jobQueue:   jobQueue,
		maxWorkers: maxWorkers,
		workerPool: workerPool,
	}
}

type Dispatcher struct {
	workerPool chan chan ServerTransferJob
	maxWorkers int
	jobQueue   <-chan ServerTransferJob
}

func (d *Dispatcher) run() {
	for i := 0; i < d.maxWorkers; i++ {
		worker := NewServerTransferWorker(i+1, d.workerPool)
		go worker.start()
	}

	go d.dispatch()
}

func (d *Dispatcher) dispatch() {
	for job := range d.jobQueue {
		go func(job ServerTransferJob) {
			log.Trace().Str("Job ID", job.ID.String()).Msg("Fetching worker job queue")
			workerJobQueue := <-d.workerPool

			log.Trace().Str("Job ID", job.ID.String()).Msg("Adding job to worker job queue")
			workerJobQueue <- job
		}(job)
	}
	for worker := range d.workerPool {
		close(worker)
	}
}
