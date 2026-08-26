package toolforge

import (
	"github.com/samber/lo"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// featureSet builds a FeatureSet from a variadic list of features.
func featureSet(features ...hostenv.Feature) hostenv.FeatureSet {
	return lo.SliceToMap(features, func(f hostenv.Feature) (hostenv.Feature, bool) {
		return f, true
	})
}

// Target creates a visible model.ToolTarget that requires all given features.
// Among all matching targets, the one with the most required features wins.
// Use it for transport-specific description variants:
//
//	Target("Upload a file...", hostenv.FeatFileHostInput, hostenv.FeatSourceURL)
func Target(desc string, features ...hostenv.Feature) model.ToolTarget {
	return model.ToolTarget{
		Require:     featureSet(features...),
		Visible:     true,
		Description: desc,
	}
}

// Fallback creates a visible model.ToolTarget with no feature requirements.
// It always matches (score 0), so it only wins when no specific target does.
// Every tool's target list should end with a Fallback to guarantee resolution.
//
//	Fallback("Upload a file...")
func Fallback(desc string) model.ToolTarget {
	return model.ToolTarget{
		Require:     hostenv.FeatureSet{},
		Visible:     true,
		Description: desc,
	}
}

// Hidden creates an invisible model.ToolTarget that suppresses the tool entirely
// for platforms matching the given features. Useful when a tool should not
// be advertised to certain hosts.
//
//	Hidden(hostenv.FeatCoLocated)
func Hidden(features ...hostenv.Feature) model.ToolTarget {
	return model.ToolTarget{
		Require: featureSet(features...),
		Visible: false,
	}
}
