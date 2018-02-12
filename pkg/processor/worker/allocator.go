/*
Copyright 2017 The Nuclio Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package worker

import (
	"sync"
	"time"

	"github.com/nuclio/nuclio/pkg/errors"

	"github.com/nuclio/logger"
)

// errors
var ErrNoAvailableWorkers = errors.New("No available workers")

type Allocator interface {

	// allocate a worker
	Allocate(timeout time.Duration) (*Worker, error)

	// release a worker
	Release(worker *Worker)

	// true if the several go routines can share this allocator
	Shareable() bool

	// get direct access to all workers for things like management / housekeeping
	GetWorkers() []*Worker
}

//
// Singleton worker
// Holds a single worker
//

type singleton struct {
	logger logger.Logger
	worker *Worker
}

func NewSingletonWorkerAllocator(parentLogger logger.Logger, worker *Worker) (Allocator, error) {

	return &singleton{
		logger: parentLogger.GetChild("singleton_allocator"),
		worker: worker,
	}, nil
}

func (s *singleton) Allocate(timeout time.Duration) (*Worker, error) {
	return s.worker, nil
}

func (s *singleton) Release(worker *Worker) {
}

// true if the several go routines can share this allocator
func (s *singleton) Shareable() bool {
	return false
}

// get direct access to all workers for things like management / housekeeping
func (s *singleton) GetWorkers() []*Worker {
	return []*Worker{s.worker}
}

//
// Fixed pool of workers
// Holds a fixed number of workers.
//

type fixedPool struct {
	logger     logger.Logger
	workerChan chan *Worker
	workers    []*Worker
}

func NewFixedPoolWorkerAllocator(parentLogger logger.Logger, workers []*Worker) (Allocator, error) {

	newFixedPool := fixedPool{
		logger:     parentLogger.GetChild("fixed_pool_allocator"),
		workerChan: make(chan *Worker, len(workers)),
		workers:    workers,
	}

	// iterate over workers, shove to pool
	for _, workerInstance := range workers {
		newFixedPool.workerChan <- workerInstance
	}

	return &newFixedPool, nil
}

func (fp *fixedPool) Allocate(timeout time.Duration) (*Worker, error) {
	select {
	case workerInstance := <-fp.workerChan:
		return workerInstance, nil
	default:
		return nil, ErrNoAvailableWorkers
	}
}

func (fp *fixedPool) Release(worker *Worker) {
	fp.workerChan <- worker
}

// true if the several go routines can share this allocator
func (fp *fixedPool) Shareable() bool {
	return true
}

// get direct access to all workers for things like management / housekeeping
func (fp *fixedPool) GetWorkers() []*Worker {
	return fp.workers
}

//
// Unbound pool of workers
// Allocates and releases workers on demand. A new worker is created each time Allocate is called.
// When Release is called, the worker's reference gets deleted and it will be GC'd later.
// Access to workers must be synchronized since this pool can be shared by goroutines.
//

type unboundPool struct {
	logger                logger.Logger
	workers               []*Worker
	workersLock           *sync.Mutex
	workerCreationFunc    workerCreator
	nextUnusedWorkerIndex int
}

type workerCreator func(index int) (*Worker, error)

func NewUnboundPoolWorkerAllocator(parentLogger logger.Logger, workerCreationFunc workerCreator) (Allocator, error) {
	newUnboundPool := unboundPool{
		logger:             parentLogger.GetChild("unbound_pool_allocator"),
		workerCreationFunc: workerCreationFunc,
	}

	return &newUnboundPool, nil
}

func (up *unboundPool) Allocate(timeout time.Duration) (*Worker, error) {
	up.workersLock.Lock()
	defer up.workersLock.Unlock()

	newWorker, err := up.workerCreationFunc(up.nextUnusedWorkerIndex)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to create worker")
	}

	// if our worker slice doesn't yet have the index we're using, extend it
	if len(up.workers) == up.nextUnusedWorkerIndex {
		up.workers = append(up.workers, newWorker)
		up.nextUnusedWorkerIndex++
	} else {

		// otherwise, put the worker into the slice and find the next unused index
		up.workers[up.nextUnusedWorkerIndex] = newWorker

		for index, worker := range up.workers {

			// if there was an unused index, set it and return the worker
			if worker == nil {
				up.nextUnusedWorkerIndex = index

				return newWorker, nil
			}
		}

		// if we got here, all indexes are used and thus the next allocation will extend the slice
		up.nextUnusedWorkerIndex = len(up.workers)
	}

	return newWorker, nil
}

func (up *unboundPool) Release(worker *Worker) {
	up.workersLock.Lock()
	defer up.workersLock.Unlock()

	for index, iteratedWorker := range up.workers {
		if worker == iteratedWorker {

			// only remove the worker - Allocate will find that the index is unused some time later
			up.workers[index] = nil

			return
		}
	}
}

// true if the several go routines can share this allocator
func (up *unboundPool) Shareable() bool {
	return true
}

// get direct access to all workers for things like management / housekeeping
func (up *unboundPool) GetWorkers() []*Worker {

	up.workersLock.Lock()
	defer up.workersLock.Unlock()

	var result []*Worker

	// we'll want to only return non-nil workers (as we may have unused indices)
	for _, worker := range up.workers {
		if worker != nil {
			result = append(result, worker)
		}
	}

	return result
}
