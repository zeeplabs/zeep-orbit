package skills

import (
	"context"
	"errors"
)

// ErrNotAvailable is returned by Source.Get for a name that is unknown,
// disabled or deleted. The tool turns it into a model-visible error; any
// other error is an internal failure and is not shown to the model.
var ErrNotAvailable = errors.New("skills: skill not available")

// Loaded is a skill ready to be handed to the model.
type Loaded struct {
	Name    string
	Source  Origin
	Version int
	Body    string
}

// Source is where the agent finds skills. The dashboard implements it over
// the embedded built-ins plus Postgres; the eval runner uses a fixture. Both
// methods see only skills that are enabled right now.
type Source interface {
	// Entries lists the enabled skills with their descriptions, never bodies.
	Entries(ctx context.Context) ([]Entry, error)
	// Get returns the latest version of an enabled skill, or ErrNotAvailable.
	Get(ctx context.Context, name string) (Loaded, error)
}
