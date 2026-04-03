package worker

import "sync"

type Job[T any] struct {
	Input T
}

type Result[I, O any] struct {
	Input  I
	Output O
	Err    error
}

func RunPool[I, O any](
	jobs []I,
	workerCount int,
	fn func(I) (O, error),
) []Result[I, O] {
	jobCh := make(chan I, len(jobs))
	resultCh := make(chan Result[I, O], len(jobs))

	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for input := range jobCh {
				out, err := fn(input)
				resultCh <- Result[I, O]{Input: input, Output: out, Err: err}
			}
		}()

	}

	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var results []Result[I, O]
	for r := range resultCh {
		results = append(results, r)
	}
	return results
}
