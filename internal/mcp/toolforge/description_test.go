package toolforge

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// profileWith returns a PlatformProfile carrying exactly the given features.
func profileWith(features ...hostenv.Feature) hostenv.PlatformProfile {
	fs := make(hostenv.FeatureSet, len(features))
	for _, f := range features {
		fs[f] = true
	}
	return hostenv.PlatformProfile{Features: fs}
}

func TestDescBuilderStaticOnly(t *testing.T) {
	d := Static("Upload a file and pin it.")
	require.Equal(t, "Upload a file and pin it.", d.Resolve(hostenv.PlatformProfile{}))
}

func TestDescBuilderWhenMatches(t *testing.T) {
	d := Static("Preamble.").When(hostenv.FeatFileHostInput, "must use `file`.")
	require.Equal(t, "Preamble. must use `file`.", d.Resolve(profileWith(hostenv.FeatFileHostInput)))
}

func TestDescBuilderWhenSkipsAbsentFeature(t *testing.T) {
	d := Static("Preamble.").When(hostenv.FeatFileHostInput, "must use `file`.")
	require.Equal(t, "Preamble.", d.Resolve(profileWith()))
	require.Equal(t, "Preamble.", d.Resolve(profileWith(hostenv.FeatSourceMint)))
}

func TestDescBuilderWhenAllRequiresEveryFeature(t *testing.T) {
	d := Static("P").WhenAll([]hostenv.Feature{hostenv.FeatSourceURL, hostenv.FeatSourceData}, "both.")
	require.Equal(t, "P both.", d.Resolve(profileWith(hostenv.FeatSourceURL, hostenv.FeatSourceData)))
	// Missing one of the required features → segment skipped.
	require.Equal(t, "P", d.Resolve(profileWith(hostenv.FeatSourceURL)))
	require.Equal(t, "P", d.Resolve(profileWith()))
}

func TestDescBuilderWhenAnyMatchesOnOne(t *testing.T) {
	d := Static("P").WhenAny([]hostenv.Feature{hostenv.FeatSourceURL, hostenv.FeatSourceData}, "relay.")
	for _, f := range []hostenv.Feature{hostenv.FeatSourceURL, hostenv.FeatSourceData} {
		require.Equal(t, "P relay.", d.Resolve(profileWith(f)))
	}
	require.Equal(t, "P", d.Resolve(profileWith()))
}

func TestDescBuilderUnlessSkipsMatchingFeature(t *testing.T) {
	d := Static("P").Unless(hostenv.FeatSinkDrop, "no drop.")
	require.Equal(t, "P no drop.", d.Resolve(profileWith()))
	require.Equal(t, "P no drop.", d.Resolve(profileWith(hostenv.FeatSinkLocal)))
	require.Equal(t, "P", d.Resolve(profileWith(hostenv.FeatSinkDrop)))
}

func TestDescBuilderUnlessSentenceStartsWithSentence(t *testing.T) {
	// UnlessSentence mirrors WhenSentence for the negation: it begins a new
	// sentence (". ") when the feature is absent, so an if/else pair reads with
	// the same joining language on both branches.
	d := Static("Call upload_file").
		WhenSentence(hostenv.FeatFileHostInput, "Prefer the file parameter.").
		UnlessSentence(hostenv.FeatFileHostInput, "This host has no file parameter.")
	require.Equal(t, "Call upload_file. This host has no file parameter.", d.Resolve(profileWith()))
	require.Equal(t, "Call upload_file. Prefer the file parameter.", d.Resolve(profileWith(hostenv.FeatFileHostInput)))
}

func TestDescBuilderIfElsePattern(t *testing.T) {
	// The if/else idiom: a When for present + Unless for absent.
	d := Static("byte source").
		When(hostenv.FeatSinkDrop, "with drop.").
		Unless(hostenv.FeatSinkDrop, "with local only.")
	require.Equal(t, "byte source with drop.", d.Resolve(profileWith(hostenv.FeatSinkDrop)))
	require.Equal(t, "byte source with local only.", d.Resolve(profileWith()))
}

func TestDescBuilderCharsMidSentence(t *testing.T) {
	// Use the *Sep variants to continue a sentence with no space separator:
	// the default separator is a space, so WhenSep("", ...) appends without one.
	d := Static("call upload_file").
		UnlessSep("", hostenv.FeatFileHostInput, "with the host file argument and archive_mode=convert").
		WhenSep("", hostenv.FeatFileHostInput, "with a convert source").
		Static(".")
	require.Equal(t, "call upload_filewith the host file argument and archive_mode=convert .",
		d.Resolve(profileWith()))
	require.Equal(t, "call upload_filewith a convert source .", d.Resolve(profileWith(hostenv.FeatFileHostInput)))
}

func TestDescBuilderDefaultSeparatorIsSpace(t *testing.T) {
	// The first segment carries no separator; subsequent segments are joined
	// with a single space by default.
	d := Static("one").Static("two").When(hostenv.FeatSourceMint, "three")
	require.Equal(t, "one two three", d.Resolve(profileWith(hostenv.FeatSourceMint)))
}

func TestDescBuilderEmptySepJoinsDirectly(t *testing.T) {
	d := Static("").StaticSep("", "a").StaticSep("", "b")
	require.Equal(t, "ab", d.Resolve(hostenv.PlatformProfile{}))
}

func TestDescBuilderFirstSegmentNeverGetsSeparator(t *testing.T) {
	// The leading Static is the first segment: any separator it declares is
	// ignored because there's no prior content.
	d := Static("").StaticSep("X", "only")
	require.Equal(t, "only", d.Resolve(hostenv.PlatformProfile{}))
}

func TestDescBuilderOrderPreserved(t *testing.T) {
	d := Static("1").
		When(hostenv.FeatSourcePath, "2a").
		When(hostenv.FeatSourceMint, "2b").
		Static("3")
	out := d.Resolve(profileWith(hostenv.FeatSourceMint))
	require.Equal(t, "1 2b 3", out)
}

func TestDescBuilderReusesBuilderAcrossProfiles(t *testing.T) {
	// A DescBuilder is immutable (methods return a new value), so the same
	// builder must resolve differently per profile.
	d := Static("P").When(hostenv.FeatSourcePath, "path").When(hostenv.FeatSourceMint, "mint")
	require.Equal(t, "P path", d.Resolve(profileWith(hostenv.FeatSourcePath)))
	require.Equal(t, "P mint", d.Resolve(profileWith(hostenv.FeatSourceMint)))
	require.Equal(t, "P", d.Resolve(profileWith()))
	// Re-resolving with the same profile yields the same result (no state).
	require.Equal(t, "P path", d.Resolve(profileWith(hostenv.FeatSourcePath)))
}

func TestSeparatorConstants(t *testing.T) {
	require.Equal(t, "", SepNone)
	require.Equal(t, " ", SepSpace)
	require.Equal(t, ". ", SepSentence)
	require.Equal(t, "; ", SepClause)
	require.Equal(t, ", ", SepList)
	require.Equal(t, " — ", SepDash)
}

func TestWhenSentence(t *testing.T) {
	d := Static("pinned").WhenSentence(hostenv.FeatSourceMint, "poll the upload handle.")
	require.Equal(t, "pinned. poll the upload handle.", d.Resolve(profileWith(hostenv.FeatSourceMint)))
	require.Equal(t, "pinned", d.Resolve(profileWith()))
}

func TestStaticSentence(t *testing.T) {
	d := Static("pinned").StaticSentence("site bundles become directory DAGs.")
	require.Equal(t, "pinned. site bundles become directory DAGs.", d.Resolve(hostenv.PlatformProfile{}))
}

func TestWhenClause(t *testing.T) {
	d := Static("writes to a host path").WhenClause(hostenv.FeatSinkDrop, "drop returns a GET link.")
	require.Equal(t, "writes to a host path; drop returns a GET link.", d.Resolve(profileWith(hostenv.FeatSinkDrop)))
	require.Equal(t, "writes to a host path", d.Resolve(profileWith()))
}

func TestWhenListAndStaticList(t *testing.T) {
	d := Static("use the host file").WhenList(hostenv.FeatSourcePath, "or a path source").StaticList("not individual assets.")
	require.Equal(t, "use the host file, or a path source, not individual assets.", d.Resolve(profileWith(hostenv.FeatSourcePath)))
	require.Equal(t, "use the host file, not individual assets.", d.Resolve(profileWith()))
}

func TestWhenDash(t *testing.T) {
	d := Static("no curl needed").WhenDash(hostenv.FeatSourceMint, "the host already owns it.")
	require.Equal(t, "no curl needed — the host already owns it.", d.Resolve(profileWith(hostenv.FeatSourceMint)))
}

func TestResolveSegments(t *testing.T) {
	d := Static("preamble").
		When(hostenv.FeatSourcePath, "path clause").
		When(hostenv.FeatSourceMint, "mint clause").
		Static("suffix")
	segs := d.ResolveSegments(profileWith(hostenv.FeatSourcePath))
	require.Equal(t, []string{"preamble", "path clause", "suffix"}, segs)
	// Segments are returned unjoined and unsubstituted (structural view).
	segs = d.ResolveSegments(profileWith(hostenv.FeatSourceMint))
	require.Equal(t, []string{"preamble", "mint clause", "suffix"}, segs)
}

func TestWhenRunAndUnlessRun(t *testing.T) {
	// Run joins with no separator — e.g. a trailing period appended directly.
	d := Static("disk").WhenRun(hostenv.FeatSinkDrop, " ; via drop").UnlessRun(hostenv.FeatSinkDrop, ".")
	require.Equal(t, "disk ; via drop", d.Resolve(profileWith(hostenv.FeatSinkDrop)))
	require.Equal(t, "disk.", d.Resolve(profileWith()))
}

func TestSentences(t *testing.T) {
	d := Static("a.").Sentences("b.", "c.", "d.")
	got := d.Resolve(profileWith())
	require.Equal(t, "a. b. c. d.", got, "self-punctuated sentences join with single spaces, no double periods")
	require.NotContains(t, got, "..")
}

func TestSentencesWhen(t *testing.T) {
	d := Static("a.").Sentences("b.").SentencesWhen(hostenv.FeatSourceMint, "c.", "d.")
	require.Equal(t, "a. b. c. d.", d.Resolve(profileWith(hostenv.FeatSourceMint)))
	require.Equal(t, "a. b.", d.Resolve(profileWith()), "block drops entirely when the gate feature is absent")
}

func TestSentencesUnless(t *testing.T) {
	d := Static("a.").SentencesUnless(hostenv.FeatSourceMint, "b.")
	require.Equal(t, "a.", d.Resolve(profileWith(hostenv.FeatSourceMint)))
	require.Equal(t, "a. b.", d.Resolve(profileWith()))
}

func TestSentencesWhenAny(t *testing.T) {
	d := Static("a.").SentencesWhenAny([]hostenv.Feature{hostenv.FeatSourceMint, hostenv.FeatSourcePath}, "b.")
	require.Equal(t, "a. b.", d.Resolve(profileWith(hostenv.FeatSourceMint)))
	require.Equal(t, "a. b.", d.Resolve(profileWith(hostenv.FeatSourcePath)))
	require.Equal(t, "a.", d.Resolve(profileWith(hostenv.FeatSourceURL)))
}

func TestListBuilderNumberedDropsGatedItems(t *testing.T) {
	l := List(ListNumbered).
		Intro("Pick the byte route in this order:").
		ItemWhen(hostenv.FeatSourceMint, "a local file → upload_file mint + host PUT + upload_status").
		ItemWhen(hostenv.FeatSourceURL, "a public HTTPS URL → upload_url").
		ItemWhen(hostenv.FeatSourceData, "only raw bytes → upload_data")

	// Grok: mint + url + data all present → three contiguous items.
	grok := l.Build(profileWith(hostenv.FeatSourceMint, hostenv.FeatSourceURL, hostenv.FeatSourceData))
	require.Equal(t, "Pick the byte route in this order:\n"+
		"1. a local file → upload_file mint + host PUT + upload_status\n"+
		"2. a public HTTPS URL → upload_url\n"+
		"3. only raw bytes → upload_data\n", grok)

	// Only mint → item 1 becomes "1." (renumbered, no gaps).
	mintOnly := l.Build(profileWith(hostenv.FeatSourceMint))
	require.Equal(t, "Pick the byte route in this order:\n"+
		"1. a local file → upload_file mint + host PUT + upload_status\n", mintOnly)

	// Openai tunnel (url + data, no mint) → two items renumbered 1..2.
	tunnel := l.Build(profileWith(hostenv.FeatSourceURL, hostenv.FeatSourceData))
	require.Equal(t, "Pick the byte route in this order:\n"+
		"1. a public HTTPS URL → upload_url\n"+
		"2. only raw bytes → upload_data\n", tunnel)
}

func TestListBuilderEmptyWhenNoItems(t *testing.T) {
	l := List(ListBulleted).Intro("Choices:").ItemWhen(hostenv.FeatSourceData, "data")
	require.Equal(t, "", l.Build(profileWith(hostenv.FeatSourceMint)), "no surviving items and no intro-only output")
}

func TestListBuilderBulleted(t *testing.T) {
	l := List(ListBulleted).Item("first").Item("second")
	require.Equal(t, "- first\n- second\n", l.Build(hostenv.PlatformProfile{}))
}

func TestDescBuilderListBlock(t *testing.T) {
	l := List(ListNumbered).
		ItemWhen(hostenv.FeatSourceMint, "mint route").
		ItemWhen(hostenv.FeatSourceURL, "url route")
	d := Static("Choose the byte route in this order:").
		ListWhenAny([]hostenv.Feature{hostenv.FeatSourceMint, hostenv.FeatSourceURL}, l)
	got := d.Resolve(profileWith(hostenv.FeatSourceMint, hostenv.FeatSourceURL))
	require.Equal(t, "Choose the byte route in this order:\n1. mint route\n2. url route\n", got)

	// No matching feature → the whole list block is omitted.
	gotNone := d.Resolve(profileWith(hostenv.FeatSourceData))
	require.Equal(t, "Choose the byte route in this order:", gotNone)
}
