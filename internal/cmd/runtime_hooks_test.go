package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
)

func TestMultiStepRunsEveryHookInOrder(t *testing.T) {
	var calls []string
	record := func(name string) graph.StepFunc {
		return func(context.Context, graph.StepInfo, *graph.State) error {
			calls = append(calls, name)
			return nil
		}
	}

	err := multiStep(record("checkpoint"), record("report"))(
		context.Background(), graph.StepInfo{Step: 1}, graph.NewState())

	if err != nil {
		t.Fatalf("multiStep: %v", err)
	}
	if len(calls) != 2 || calls[0] != "checkpoint" || calls[1] != "report" {
		t.Errorf("calls = %v, want [checkpoint report]", calls)
	}
}

func TestMultiStepStopsAtTheFirstError(t *testing.T) {
	boom := errors.New("checkpoint unavailable")
	reached := false

	err := multiStep(
		func(context.Context, graph.StepInfo, *graph.State) error { return boom },
		func(context.Context, graph.StepInfo, *graph.State) error { reached = true; return nil },
	)(context.Background(), graph.StepInfo{Step: 1}, graph.NewState())

	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
	if reached {
		t.Error("the second hook ran after the first failed — durability must gate the rest")
	}
}

func TestMultiStepSkipsNilHooks(t *testing.T) {
	called := false

	err := multiStep(nil, func(context.Context, graph.StepInfo, *graph.State) error {
		called = true
		return nil
	}, nil)(context.Background(), graph.StepInfo{Step: 1}, graph.NewState())

	if err != nil {
		t.Fatalf("multiStep: %v", err)
	}
	if !called {
		t.Error("a nil hook in the chain swallowed the rest")
	}
}

func TestMultiStepWithNoHooksIsANoop(t *testing.T) {
	if err := multiStep()(context.Background(), graph.StepInfo{}, graph.NewState()); err != nil {
		t.Errorf("multiStep() = %v, want nil", err)
	}
}

func TestMultiEventRunsEveryHookInOrder(t *testing.T) {
	var calls []string
	record := func(name string) graph.EventFunc {
		return func(context.Context, graph.Event) error {
			calls = append(calls, name)
			return nil
		}
	}

	err := multiEvent(record("journal"), record("observer"))(
		context.Background(), graph.Event{Kind: graph.EventRunStarted})

	if err != nil {
		t.Fatalf("multiEvent: %v", err)
	}
	if len(calls) != 2 || calls[0] != "journal" || calls[1] != "observer" {
		t.Errorf("calls = %v, want [journal observer]", calls)
	}
}

func TestMultiEventStopsAtTheFirstError(t *testing.T) {
	boom := errors.New("boom")
	var reached bool

	err := multiEvent(
		func(context.Context, graph.Event) error { return boom },
		func(context.Context, graph.Event) error {
			reached = true
			return nil
		},
	)(context.Background(), graph.Event{Kind: graph.EventNodeStarted})

	if !errors.Is(err, boom) {
		t.Fatalf("multiEvent error = %v, want %v", err, boom)
	}
	if reached {
		t.Errorf("the second hook ran after the first refused the event")
	}
}

func TestMultiEventSkipsNilHooks(t *testing.T) {
	var called bool

	err := multiEvent(nil, func(context.Context, graph.Event) error {
		called = true
		return nil
	})(context.Background(), graph.Event{Kind: graph.EventRunFinished})

	if err != nil {
		t.Fatalf("multiEvent: %v", err)
	}
	if !called {
		t.Errorf("a nil hook must be skipped, not stop the chain")
	}
}

func TestMultiEventWithNoHooksIsANoop(t *testing.T) {
	if err := multiEvent()(context.Background(), graph.Event{}); err != nil {
		t.Errorf("multiEvent() = %v, want nil", err)
	}
}
