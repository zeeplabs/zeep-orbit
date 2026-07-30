package provisioner

import (
	"fmt"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// topoSortTables orders tables so that a table referenced by a foreign key
// always comes before the table(s) that reference it, using Kahn's
// algorithm. config.ValidateSchema (loader.go) already rejects cycles
// before this runs; the error path here is a second line of defense, not
// the primary validation.
func topoSortTables(tables []config.TableConfig) ([]config.TableConfig, error) {
	byName := make(map[string]config.TableConfig, len(tables))
	inDegree := make(map[string]int, len(tables))
	dependents := make(map[string][]string, len(tables))

	for _, t := range tables {
		byName[t.Name] = t
		if _, ok := inDegree[t.Name]; !ok {
			inDegree[t.Name] = 0
		}
	}

	for _, t := range tables {
		for _, c := range t.Columns {
			if c.References == nil || c.References.Table == t.Name {
				continue
			}
			if _, ok := byName[c.References.Table]; !ok {
				// Reference to a table outside this set (shouldn't happen
				// once config.ValidateSchema has run) — ignored here.
				continue
			}
			dependents[c.References.Table] = append(dependents[c.References.Table], t.Name)
			inDegree[t.Name]++
		}
	}

	var queue []string
	for _, t := range tables {
		if inDegree[t.Name] == 0 {
			queue = append(queue, t.Name)
		}
	}

	ordered := make([]config.TableConfig, 0, len(tables))
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		ordered = append(ordered, byName[name])

		for _, dep := range dependents[name] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(ordered) != len(tables) {
		return nil, fmt.Errorf("provisioner: circular foreign key dependency detected among tables")
	}

	return ordered, nil
}
