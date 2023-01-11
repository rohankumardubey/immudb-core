/*
Copyright 2022 Codenotary Inc. All rights reserved.

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

package watchers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatchersHub(t *testing.T) {
	waitessCount := 1_000

	wHub := New(0, waitessCount*2)

	wHub.DoneUpto(0)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := wHub.WaitFor(1, ctx)
	require.ErrorIs(t, err, ErrCancellationRequested)

	doneUpto, waiting, err := wHub.Status()
	require.NoError(t, err)
	require.Equal(t, uint64(0), doneUpto)
	require.Equal(t, 0, waiting)

	var wg sync.WaitGroup
	wg.Add(waitessCount * 2)

	for it := 0; it < 2; it++ {
		for i := 1; i <= waitessCount; i++ {
			go func(i uint64) {
				defer wg.Done()
				err := wHub.WaitFor(i, context.Background())
				require.NoError(t, err)
			}(uint64(i))
		}
	}

	time.Sleep(10 * time.Millisecond)

	err = wHub.WaitFor(uint64(waitessCount*2+1), context.Background())
	require.ErrorIs(t, err, ErrMaxWaitessLimitExceeded)

	done := make(chan struct{})

	go func(done <-chan struct{}) {
		id := uint64(1)

		for {
			select {
			case <-time.Tick(1 * time.Millisecond):
				{
					err := wHub.DoneUpto(id + 2)
					require.NoError(t, err)
					id++
				}
			case <-done:
				{
					return
				}
			}
		}
	}(done)

	wg.Wait()

	done <- struct{}{}

	if t.Failed() {
		t.FailNow()
	}

	err = wHub.WaitFor(5, context.Background())
	require.NoError(t, err)

	wg.Add(1)

	go func() {
		defer wg.Done()
		err := wHub.WaitFor(uint64(waitessCount)+1, context.Background())
		if !errors.Is(err, ErrAlreadyClosed) {
			require.NoError(t, err)
		}
	}()

	time.Sleep(1 * time.Millisecond)

	err = wHub.Close()
	require.NoError(t, err)

	wg.Wait()

	if t.Failed() {
		t.FailNow()
	}

	err = wHub.WaitFor(0, context.Background())
	require.ErrorIs(t, err, ErrAlreadyClosed)

	err = wHub.DoneUpto(0)
	require.ErrorIs(t, err, ErrAlreadyClosed)

	_, _, err = wHub.Status()
	require.ErrorIs(t, err, ErrAlreadyClosed)

	err = wHub.Close()
	require.ErrorIs(t, err, ErrAlreadyClosed)
}

func TestSimultaneousCancellationAndNotification(t *testing.T) {
	wHub := New(0, 30)

	const maxIterations = 100

	wg := sync.WaitGroup{}
	// Spawn waitees
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			for j := uint64(0); j < maxIterations; j++ {
				func() {
					ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
					defer cancel()

					doneUpTo, _, err := wHub.Status()
					require.NoError(t, err)

					err = wHub.WaitFor(j, ctx)
					if errors.Is(err, ErrCancellationRequested) {
						// Check internal invariant of the wHub
						// Since we got cancel request it must only happen
						// as long as we did not already cross the waiting point
						require.Less(t, doneUpTo, j)
					} else {
						require.NoError(t, err)
					}
				}()
			}
		}(i)
	}

	// Producer
	for j := uint64(1); j < maxIterations; j++ {
		wHub.DoneUpto(j)
		time.Sleep(time.Millisecond)
	}

	wg.Wait()

	assert.Zero(t, wHub.waiting)
	assert.Empty(t, wHub.wpoints)
}
