package listener_test

import (
	"fmt"
	"testing"

	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/ecs/event"
	"github.com/mlange-42/arche/listener"
	"github.com/stretchr/testify/assert"
)

func TestCallback(t *testing.T) {
	evt := []ecs.EntityEvent{}
	ls := listener.NewCallback(
		event.All,
		func(e ecs.EntityEvent) {
			evt = append(evt, e)
		},
	)
	assert.Equal(t, event.All, ls.Subscriptions())
	ls.Notify(ecs.EntityEvent{})
	assert.Equal(t, 1, len(evt))
}

func ExampleCallback() {
	world := ecs.NewWorld()

	ls := listener.NewCallback(
		event.Entities|event.Components,
		func(e ecs.EntityEvent) {
			fmt.Println(e)
		},
	)
	world.SetListener(&ls)

	world.NewEntity()
}