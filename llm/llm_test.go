package llm

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"maragu.dev/is"
)

func TestResultArchetypes(t *testing.T) {
	t.Run("has exactly 100 archetypes", func(t *testing.T) {
		is.Equal(t, 100, len(resultArchetypes))
	})

	t.Run("no empty entries", func(t *testing.T) {
		for i, a := range resultArchetypes {
			is.True(t, strings.TrimSpace(a) != "", "archetype at index", i)
		}
	})

	t.Run("no duplicates", func(t *testing.T) {
		seen := map[string]bool{}
		for _, a := range resultArchetypes {
			is.True(t, !seen[a], "duplicate archetype:", a)
			seen[a] = true
		}
	})
}

func TestReliabilitySignals(t *testing.T) {
	t.Run("has exactly 6 signals", func(t *testing.T) {
		is.Equal(t, 6, len(reliabilitySignals))
	})

	t.Run("no empty entries", func(t *testing.T) {
		for i, s := range reliabilitySignals {
			is.True(t, strings.TrimSpace(s) != "", "reliability signal at index", i)
		}
	})
}

func TestAdProductCategories(t *testing.T) {
	t.Run("has exactly 40 categories", func(t *testing.T) {
		is.Equal(t, 40, len(adProductCategories))
	})

	t.Run("no empty entries", func(t *testing.T) {
		for i, c := range adProductCategories {
			is.True(t, strings.TrimSpace(c) != "", "ad product category at index", i)
		}
	})

	t.Run("no duplicates", func(t *testing.T) {
		seen := map[string]bool{}
		for _, c := range adProductCategories {
			is.True(t, !seen[c], "duplicate product category:", c)
			seen[c] = true
		}
	})
}

func TestAdPitchAngles(t *testing.T) {
	t.Run("has exactly 15 pitch angles", func(t *testing.T) {
		is.Equal(t, 15, len(adPitchAngles))
	})

	t.Run("no empty entries", func(t *testing.T) {
		for i, a := range adPitchAngles {
			is.True(t, strings.TrimSpace(a) != "", "ad pitch angle at index", i)
		}
	})

	t.Run("no duplicates", func(t *testing.T) {
		seen := map[string]bool{}
		for _, a := range adPitchAngles {
			is.True(t, !seen[a], "duplicate pitch angle:", a)
			seen[a] = true
		}
	})
}

func TestRollResultConstraints(t *testing.T) {
	t.Run("returns a roll whose fields come from the curated lists", func(t *testing.T) {
		r := rand.New(rand.NewPCG(1, 2))
		for range 1000 {
			roll := rollResultConstraints(r)
			is.True(t, slices.Contains(resultArchetypes, roll.archetype), "archetype not in curated list:", roll.archetype)
			is.True(t, slices.Contains(reliabilitySignals, roll.reliability), "reliability not in curated list:", roll.reliability)
			is.True(t, roll.weirdness >= 1 && roll.weirdness <= 10, "weirdness out of range:", roll.weirdness)
		}
	})

	t.Run("is deterministic for a seeded RNG", func(t *testing.T) {
		r1 := rand.New(rand.NewPCG(42, 42))
		r2 := rand.New(rand.NewPCG(42, 42))
		for range 20 {
			a := rollResultConstraints(r1)
			b := rollResultConstraints(r2)
			is.Equal(t, a.archetype, b.archetype)
			is.Equal(t, a.reliability, b.reliability)
			is.Equal(t, a.weirdness, b.weirdness)
		}
	})

	t.Run("nil RNG uses the global generator and returns valid rolls", func(t *testing.T) {
		for range 50 {
			roll := rollResultConstraints(nil)
			is.True(t, slices.Contains(resultArchetypes, roll.archetype))
			is.True(t, slices.Contains(reliabilitySignals, roll.reliability))
			is.True(t, roll.weirdness >= 1 && roll.weirdness <= 10)
		}
	})
}

func TestRollAdConstraints(t *testing.T) {
	t.Run("returns a roll whose fields come from the curated lists", func(t *testing.T) {
		r := rand.New(rand.NewPCG(3, 4))
		for range 1000 {
			roll := rollAdConstraints(r)
			is.True(t, slices.Contains(adProductCategories, roll.category), "category not in curated list:", roll.category)
			is.True(t, slices.Contains(adPitchAngles, roll.pitch), "pitch not in curated list:", roll.pitch)
			is.True(t, roll.weirdness >= 1 && roll.weirdness <= 10, "weirdness out of range:", roll.weirdness)
		}
	})

	t.Run("is deterministic for a seeded RNG", func(t *testing.T) {
		r1 := rand.New(rand.NewPCG(7, 7))
		r2 := rand.New(rand.NewPCG(7, 7))
		for range 20 {
			a := rollAdConstraints(r1)
			b := rollAdConstraints(r2)
			is.Equal(t, a.category, b.category)
			is.Equal(t, a.pitch, b.pitch)
			is.Equal(t, a.weirdness, b.weirdness)
		}
	})

	t.Run("nil RNG uses the global generator and returns valid rolls", func(t *testing.T) {
		for range 50 {
			roll := rollAdConstraints(nil)
			is.True(t, slices.Contains(adProductCategories, roll.category))
			is.True(t, slices.Contains(adPitchAngles, roll.pitch))
			is.True(t, roll.weirdness >= 1 && roll.weirdness <= 10)
		}
	})
}

func TestResultConstraintsBlock(t *testing.T) {
	t.Run("contains the three rolled values under a Constraints: header", func(t *testing.T) {
		roll := resultRolls{archetype: "fake wiki article", reliability: "seems reliable", weirdness: 7}
		block := roll.constraintsBlock()
		is.True(t, strings.Contains(block, "Constraints:"), "missing Constraints header:", block)
		is.True(t, strings.Contains(block, "fake wiki article"), "missing archetype:", block)
		is.True(t, strings.Contains(block, "seems reliable"), "missing reliability:", block)
		is.True(t, strings.Contains(block, "7"), "missing weirdness:", block)
	})
}

func TestAdConstraintsBlock(t *testing.T) {
	t.Run("contains the three rolled values under a Constraints: header", func(t *testing.T) {
		roll := adRolls{category: "SaaS", pitch: "nostalgia pitch", weirdness: 3}
		block := roll.constraintsBlock()
		is.True(t, strings.Contains(block, "Constraints:"), "missing Constraints header:", block)
		is.True(t, strings.Contains(block, "SaaS"), "missing category:", block)
		is.True(t, strings.Contains(block, "nostalgia pitch"), "missing pitch:", block)
		is.True(t, strings.Contains(block, "3"), "missing weirdness:", block)
	})
}

func TestResultSystemPromptHouseStyle(t *testing.T) {
	t.Run("mentions deadpan/dry humor house style", func(t *testing.T) {
		lower := strings.ToLower(resultSystemPrompt)
		is.True(t, strings.Contains(lower, "house style"), "missing house style paragraph:", resultSystemPrompt)
		is.True(t, strings.Contains(lower, "deadpan") || strings.Contains(lower, "dry"), "missing deadpan/dry mention:", resultSystemPrompt)
	})

	t.Run("no longer contains the tone-should-vary-wildly line", func(t *testing.T) {
		is.True(t, !strings.Contains(resultSystemPrompt, "Tone should vary wildly"), "old tone line still present:", resultSystemPrompt)
	})
}

func TestAdSystemPromptHouseStyle(t *testing.T) {
	t.Run("mentions deadpan/dry humor house style", func(t *testing.T) {
		lower := strings.ToLower(adSystemPrompt)
		is.True(t, strings.Contains(lower, "house style"), "missing house style paragraph:", adSystemPrompt)
		is.True(t, strings.Contains(lower, "deadpan") || strings.Contains(lower, "dry"), "missing deadpan/dry mention:", adSystemPrompt)
	})

	t.Run("no longer contains the tone-should-vary-wildly line", func(t *testing.T) {
		is.True(t, !strings.Contains(adSystemPrompt, "Tone should vary wildly"), "old tone line still present:", adSystemPrompt)
	})
}
