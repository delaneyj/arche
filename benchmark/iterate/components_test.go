package iterate

import (
	"testing"

	"github.com/mlange-42/arche/ecs"
)

func TestRegisterAll(t *testing.T) {
	w := ecs.NewWorld()
	registerAll(&w)
}
