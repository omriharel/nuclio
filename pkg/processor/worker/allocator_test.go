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
	"testing"
	"time"

	"github.com/nuclio/logger"
	"github.com/nuclio/zap"
	"github.com/stretchr/testify/suite"
)

type AllocatorTestSuite struct {
	suite.Suite
	logger logger.Logger
}

func (suite *AllocatorTestSuite) SetupSuite() {
	suite.logger, _ = nucliozap.NewNuclioZapTest("test")
}

func (suite *AllocatorTestSuite) TestSingletonAllocator() {
	worker1 := &Worker{}

	sa, err := NewSingletonWorkerAllocator(suite.logger, worker1)
	suite.Require().NoError(err)
	suite.Require().NotNil(sa)

	// allocate once, time should be ignored
	allocatedWorker, err := sa.Allocate(time.Hour)
	suite.Require().NoError(err)
	suite.Require().Equal(worker1, allocatedWorker)

	// allocate again, release doesn't need to happen
	allocatedWorker, err = sa.Allocate(time.Hour)
	suite.Require().NoError(err)
	suite.Require().Equal(worker1, allocatedWorker)

	// release shouldn't do anything
	suite.Require().NotPanics(func() { sa.Release(worker1) })

	suite.Require().False(sa.Shareable())
}

func (suite *AllocatorTestSuite) TestFixedPoolAllocator() {
	worker1 := &Worker{index: 0}
	worker2 := &Worker{index: 1}
	workers := []*Worker{worker1, worker2}

	fpa, err := NewFixedPoolWorkerAllocator(suite.logger, workers)
	suite.Require().NoError(err)
	suite.Require().NotNil(fpa)

	// allocate once - should allocate
	firstAllocatedWorker, err := fpa.Allocate(time.Hour)
	suite.Require().NoError(err)
	suite.Require().Contains(workers, firstAllocatedWorker)

	// allocate again - should allocate other worker
	secondAllocatedWorker, err := fpa.Allocate(time.Hour)
	suite.Require().NoError(err)
	suite.Require().Contains(workers, secondAllocatedWorker)
	suite.Require().NotEqual(firstAllocatedWorker, secondAllocatedWorker)

	// allocate yet again - should time out
	failedAllocationWorker, err := fpa.Allocate(50 * time.Millisecond)
	suite.Require().Error(err)
	suite.Require().Nil(failedAllocationWorker)

	// release the second worker
	suite.Require().NotPanics(func() { fpa.Release(worker2) })

	// allocate again - should allocate second worker
	thirdAllocatedWorker, err := fpa.Allocate(time.Hour)
	suite.Require().NoError(err)
	suite.Require().Equal(worker2, thirdAllocatedWorker)

	suite.Require().True(fpa.Shareable())
}

func (suite *AllocatorTestSuite) TestUnboundPoolAllocator() {
	workerCreationFunc := func(index int) (*Worker, error) {
		return &Worker{
			index: index,
		}, nil
	}

	upa, err := NewUnboundPoolWorkerAllocator(suite.logger, workerCreationFunc)
	suite.Require().NoError(err)
	suite.Require().NotNil(upa)

	// allocate once - should allocate
	firstAllocatedWorker, err := upa.Allocate(time.Hour)
	suite.Require().NoError(err)
	suite.Require().Contains(upa.GetWorkers(), firstAllocatedWorker)
	suite.Require().Equal(firstAllocatedWorker.index, 0)

	// allocate again - should allocate other worker
	secondAllocatedWorker, err := upa.Allocate(time.Hour)
	suite.Require().NoError(err)
	suite.Require().Contains(upa.GetWorkers(), secondAllocatedWorker)
	suite.Require().NotEqual(firstAllocatedWorker, secondAllocatedWorker)
	suite.Require().Equal(secondAllocatedWorker.index, 1)

	// release the second worker
	suite.Require().NotPanics(func() { upa.Release(secondAllocatedWorker) })
	suite.Require().NotContains(upa.GetWorkers(), secondAllocatedWorker)
	suite.Require().Len(upa.GetWorkers(), 1)

	// allocate again - should allocate second worker
	thirdAllocatedWorker, err := upa.Allocate(time.Hour)
	suite.Require().NoError(err)
	suite.Require().Contains(upa.GetWorkers(), thirdAllocatedWorker)

	// the third allocated worker should have index 1 because we freed it by releasing the second allocated worker
	suite.Require().Equal(thirdAllocatedWorker.index, 1)

	// release the first worker
	suite.Require().NotPanics(func() { upa.Release(firstAllocatedWorker) })
	suite.Require().NotContains(upa.GetWorkers(), firstAllocatedWorker)
	suite.Require().Len(upa.GetWorkers(), 1)

	// allocate a fourth worker - should have index 0
	fourthAllocatedWorker, err := upa.Allocate(time.Hour)
	suite.Require().NoError(err)
	suite.Require().Contains(upa.GetWorkers(), fourthAllocatedWorker)
	suite.Require().Equal(fourthAllocatedWorker.index, 0)

	suite.Require().True(upa.Shareable())
}

func TestAllocatorTestSuite(t *testing.T) {
	suite.Run(t, new(AllocatorTestSuite))
}
