// Evaluates ssh_config Match lines (ssh_config(5), Match) as far as
// mitosu can: all, host, originalhost, user and localuser. The other
// criteria (exec, localnetwork, canonical, final, tagged, command,
// version, sessiontype) depend on things mitosu does not do or know.

package ssh

import (
	"fmt"
	"strings"
)

// matchArgCriteria take one argument; the rest (all, canonical, final)
// take none.
var matchArgCriteria = map[string]bool{
	"exec": true, "localnetwork": true, "host": true, "originalhost": true,
	"tagged": true, "command": true, "user": true, "localuser": true,
	"version": true, "sessiontype": true,
}

// matchState returns the block state of a Match line at where: active if
// every criterion holds, inactive if one evaluable criterion fails, and
// unknown if all evaluable ones hold but another cannot be evaluated (a
// warning is recorded). Unknown criteria names and missing arguments are
// errors, as in OpenSSH.
func (r *resolver) matchState(args []string, where string) (blockState, error) {
	matches := true
	unsupported := ""
	for i := 0; i < len(args); i++ {
		criterion := strings.ToLower(args[i])
		negate := strings.HasPrefix(criterion, "!")
		criterion = strings.TrimPrefix(criterion, "!")

		if criterion == "all" {
			continue
		}
		if criterion == "canonical" || criterion == "final" {
			unsupported = criterion
			continue
		}
		if !matchArgCriteria[criterion] {
			return blockState{}, fmt.Errorf("ssh config %s: unknown Match criterion %q", where, args[i])
		}
		if i+1 >= len(args) {
			return blockState{}, fmt.Errorf("ssh config %s: Match %s needs an argument", where, criterion)
		}
		i++
		pattern := args[i]

		var subject string
		switch criterion {
		case "host":
			h, err := r.hostname()
			if err != nil {
				return blockState{}, err
			}
			subject = h
		case "originalhost":
			subject = r.name
		case "user":
			subject = r.first("user")
			if subject == "" {
				u, err := LocalUser()
				if err != nil {
					return blockState{}, err
				}
				subject = u
			}
		case "localuser":
			u, err := LocalUser()
			if err != nil {
				return blockState{}, err
			}
			subject = u
		default:
			unsupported = criterion
			continue
		}
		if patternListMatches(pattern, subject) == negate {
			matches = false
		}
	}

	if !matches {
		return blockState{}, nil
	}
	if unsupported != "" {
		r.warnings = append(r.warnings, fmt.Sprintf("%s: Match %s is not supported; the block is skipped", where, unsupported))
		return blockState{unknown: where}, nil
	}
	return blockState{active: true}, nil
}
