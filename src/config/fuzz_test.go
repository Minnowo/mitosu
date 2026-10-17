// Fuzz target for mitosu.conf: any text gives a config or an error.

package config

import "testing"

func FuzzParse(f *testing.F) {
	f.Add(fullConfig)
	f.Add("tier = raw 30d\ntier = 1m 1y\ntier = 1h forever\npull_interval = 5m\n")
	f.Fuzz(func(t *testing.T, text string) {
		_, _ = Parse(text)
	})
}
