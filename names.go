package bcl

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// sameCollectionName reports whether fieldName and blockType should be treated
// as the same logical BCL collection/member name.
//
// It is intentionally schema/parser friendly:
//   - exact match:       cases == cases
//   - plural/singular:   cases == case
//   - snake/kebab/camel: user_roles == userRole == user-role
//   - dotted paths:      auth.rules == auth.rule
//   - acronyms:          APIKeys == api_key
//   - irregulars:        people == person, children == child
//   - uncountables:      metadata == metadata
//
// Use this anywhere BCL needs to decide whether repeated singular blocks should
// hydrate a plural collection field:
//
//	cases [
//	  ...
//	]
//
// and:
//
//	case "role membership" {
//	  ...
//	}
//
// should both map to the same schema/model field.
func sameCollectionName(fieldName, blockType string) bool {
	return defaultNameInflector.SameCollectionName(fieldName, blockType)
}

// singularName returns the singular form of the final token in name while
// preserving the original separator/case shape when possible.
func singularName(name string) string {
	return defaultNameInflector.Singular(name)
}

// pluralName returns the plural form of the final token in name while preserving
// the original separator/case shape when possible.
func pluralName(name string) string {
	return defaultNameInflector.Plural(name)
}

// preserveNameCase keeps compatibility with the older helper. Prefer using
// defaultNameInflector.applyTokenStyle for new code.
func preserveNameCase(original, normalized string) string {
	return applySimpleCase(original, normalized)
}

// CollectionNameInflector is a production-safe name normalizer/inflector used
// by BCL schema matching, marshal, and unmarshal paths.
//
// It is deliberately dependency-free and deterministic. It is not intended to be
// a full natural-language inflector. It is designed for configuration keys,
// schema fields, and block names.
type CollectionNameInflector struct {
	mu sync.RWMutex

	aliases      map[string]string
	irregular    map[string]string
	reverseIrreg map[string]string
	uncountable  map[string]struct{}
	acronyms     map[string]string

	singularCache map[string]string
	pluralCache   map[string]string
	keyCache      map[string]string
}

// NameInflectorOption customizes a CollectionNameInflector.
type NameInflectorOption func(*CollectionNameInflector)

// NewCollectionNameInflector creates a configurable inflector.
//
// Most callers should use the package-level helpers, but this is useful for
// projects that want custom domain names:
//
//	inf := NewCollectionNameInflector(
//		WithNameAlias("acl", "access_control_list"),
//		WithIrregularName("criterion", "criteria"),
//		WithUncountableName("series"),
//	)
func NewCollectionNameInflector(opts ...NameInflectorOption) *CollectionNameInflector {
	i := &CollectionNameInflector{
		aliases:       make(map[string]string, 128),
		irregular:     make(map[string]string, 128),
		reverseIrreg:  make(map[string]string, 128),
		uncountable:   make(map[string]struct{}, 128),
		acronyms:      make(map[string]string, 64),
		singularCache: make(map[string]string, 512),
		pluralCache:   make(map[string]string, 512),
		keyCache:      make(map[string]string, 512),
	}

	for _, pair := range defaultIrregularPairs {
		i.addIrregular(pair[0], pair[1])
	}
	for _, name := range defaultUncountables {
		i.addUncountable(name)
	}
	for _, acronym := range defaultAcronyms {
		i.addAcronym(acronym)
	}
	for _, pair := range defaultAliases {
		i.addAlias(pair[0], pair[1])
	}

	for _, opt := range opts {
		if opt != nil {
			opt(i)
		}
	}

	return i
}

// WithNameAlias makes multiple names equivalent after normalization.
// Example: WithNameAlias("id", "identifier") makes id/ids match identifier/identifiers.
func WithNameAlias(from, to string) NameInflectorOption {
	return func(i *CollectionNameInflector) {
		i.addAlias(from, to)
	}
}

// WithIrregularName registers singular -> plural.
// Example: WithIrregularName("criterion", "criteria").
func WithIrregularName(singular, plural string) NameInflectorOption {
	return func(i *CollectionNameInflector) {
		i.addIrregular(singular, plural)
	}
}

// WithUncountableName registers a name whose singular and plural forms are the same.
// Example: WithUncountableName("metadata").
func WithUncountableName(name string) NameInflectorOption {
	return func(i *CollectionNameInflector) {
		i.addUncountable(name)
	}
}

// WithAcronym improves tokenization/case preservation for names like APIKeys,
// OAuthClients, HTTPRoutes.
func WithAcronym(name string) NameInflectorOption {
	return func(i *CollectionNameInflector) {
		i.addAcronym(name)
	}
}

var defaultNameInflector = NewCollectionNameInflector()

var defaultAliases = [][2]string{
	{"id", "identifier"},
	{"ids", "identifiers"},
	{"uuid", "identifier"},
	{"uids", "identifiers"},
	{"cfg", "config"},
	{"conf", "config"},
	{"configs", "configurations"},
	{"spec", "specification"},
	{"specs", "specifications"},
	{"expr", "expression"},
	{"exprs", "expressions"},
	{"env", "environment"},
	{"vars", "variables"},
	{"param", "parameter"},
	{"params", "parameters"},
	{"arg", "argument"},
	{"args", "arguments"},
	{"attr", "attribute"},
	{"attrs", "attributes"},
	{"prop", "property"},
	{"props", "properties"},
	{"ref", "reference"},
	{"refs", "references"},
	{"authz", "authorization"},
	{"authn", "authentication"},
}

var defaultAcronyms = []string{
	"ACL",
	"API",
	"ARN",
	"ASCII",
	"CPU",
	"CSS",
	"CSV",
	"DNS",
	"GUID",
	"HTML",
	"HTTP",
	"HTTPS",
	"ID",
	"IP",
	"JSON",
	"JWT",
	"OAuth",
	"OIDC",
	"RAM",
	"RPC",
	"SAML",
	"SDK",
	"SQL",
	"SSH",
	"SSL",
	"TCP",
	"TLS",
	"TTL",
	"UID",
	"URI",
	"URL",
	"UUID",
	"VM",
	"XML",
	"YAML",
}

// defaultIrregularPairs are singular -> plural.
//
// Keep this list focused on words commonly used in config/schema/model names.
// It avoids surprising transformations for general prose.
var defaultIrregularPairs = [][2]string{
	{"addendum", "addenda"},
	{"alias", "aliases"},
	{"analysis", "analyses"},
	{"appendix", "appendices"},
	{"axis", "axes"},
	{"basis", "bases"},
	{"child", "children"},
	{"criterion", "criteria"},
	{"crisis", "crises"},
	{"datum", "data"},
	{"diagnosis", "diagnoses"},
	{"ellipsis", "ellipses"},
	{"focus", "foci"},
	{"foot", "feet"},
	{"formula", "formulae"},
	{"goose", "geese"},
	{"hypothesis", "hypotheses"},
	{"index", "indices"},
	{"leaf", "leaves"},
	{"life", "lives"},
	{"loaf", "loaves"},
	{"man", "men"},
	{"matrix", "matrices"},
	{"medium", "media"},
	{"mouse", "mice"},
	{"oasis", "oases"},
	{"octopus", "octopi"},
	{"ox", "oxen"},
	{"parenthesis", "parentheses"},
	{"person", "people"},
	{"phenomenon", "phenomena"},
	{"radius", "radii"},
	{"self", "selves"},
	{"status", "statuses"},
	{"stimulus", "stimuli"},
	{"syllabus", "syllabi"},
	{"thesis", "theses"},
	{"tooth", "teeth"},
	{"vertex", "vertices"},
	{"wife", "wives"},
	{"wolf", "wolves"},
	{"woman", "women"},
}

var defaultUncountables = []string{
	"access",
	"advice",
	"air",
	"analytics",
	"bcl",
	"cache",
	"compliance",
	"config",
	"data",
	"equipment",
	"evidence",
	"feedback",
	"fish",
	"garbage",
	"hardware",
	"health",
	"help",
	"homework",
	"information",
	"knowledge",
	"metadata",
	"money",
	"news",
	"permission",
	"policy",
	"research",
	"rice",
	"series",
	"sheep",
	"software",
	"species",
	"traffic",
	"transport",
	"water",
	"work",
}

// SameCollectionName reports whether two names are equivalent as collection/member names.
func (i *CollectionNameInflector) SameCollectionName(fieldName, blockType string) bool {
	a := strings.TrimSpace(fieldName)
	b := strings.TrimSpace(blockType)
	if a == "" || b == "" {
		return a == b
	}
	if a == b {
		return true
	}

	ak := i.CollectionKey(a)
	bk := i.CollectionKey(b)
	if ak == bk {
		return true
	}

	as := i.CollectionKey(i.Singular(a))
	bs := i.CollectionKey(i.Singular(b))
	if as == bs {
		return true
	}

	ap := i.CollectionKey(i.Plural(a))
	bp := i.CollectionKey(i.Plural(b))
	if ap == bp {
		return true
	}

	return as == bp || ap == bs
}

// CollectionKey returns a stable comparison key for matching schema fields,
// struct tags, and block names. It normalizes separators, case, aliases,
// singular/plural shape, and acronym usage.
func (i *CollectionNameInflector) CollectionKey(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	i.mu.RLock()
	if v, ok := i.keyCache[name]; ok {
		i.mu.RUnlock()
		return v
	}
	i.mu.RUnlock()

	parts := splitPath(name)
	for p := range parts {
		tokens := splitNameTokens(parts[p])
		for t := range tokens {
			token := strings.ToLower(tokens[t])
			token = i.aliasToken(token)
			token = i.singularToken(token)
			tokens[t] = token
		}
		parts[p] = strings.Join(tokens, "_")
	}

	key := strings.Join(parts, ".")

	i.mu.Lock()
	i.keyCache[name] = key
	i.mu.Unlock()

	return key
}

// Singular singularizes the final token of a name while preserving path,
// separator, and broad case style.
//
// Examples:
//   - cases           -> case
//   - APIKeys         -> APIKey
//   - user_roles      -> user_role
//   - auth.rules      -> auth.rule
//   - request-statuses -> request-status
func (i *CollectionNameInflector) Singular(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	i.mu.RLock()
	if v, ok := i.singularCache[name]; ok {
		i.mu.RUnlock()
		return v
	}
	i.mu.RUnlock()

	result := i.transformFinalToken(name, i.singularToken)

	i.mu.Lock()
	i.singularCache[name] = result
	i.mu.Unlock()

	return result
}

// Plural pluralizes the final token of a name while preserving path,
// separator, and broad case style.
//
// Examples:
//   - case            -> cases
//   - APIKey          -> APIKeys
//   - user_role       -> user_roles
//   - auth.rule       -> auth.rules
//   - request-status  -> request-statuses
func (i *CollectionNameInflector) Plural(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	i.mu.RLock()
	if v, ok := i.pluralCache[name]; ok {
		i.mu.RUnlock()
		return v
	}
	i.mu.RUnlock()

	result := i.transformFinalToken(name, i.pluralToken)

	i.mu.Lock()
	i.pluralCache[name] = result
	i.mu.Unlock()

	return result
}

func (i *CollectionNameInflector) transformFinalToken(name string, fn func(string) string) string {
	pathPrefix, leaf := splitLastPath(name)
	if leaf == "" {
		return name
	}

	spans := scanNameSpans(leaf)
	tokenIdx := -1
	for idx := len(spans) - 1; idx >= 0; idx-- {
		if spans[idx].kind == spanToken {
			tokenIdx = idx
			break
		}
	}
	if tokenIdx < 0 {
		return name
	}

	originalToken := spans[tokenIdx].text
	normalized := fn(strings.ToLower(originalToken))
	spans[tokenIdx].text = applyTokenStyle(originalToken, normalized)

	var b strings.Builder
	b.Grow(len(name) + 8)
	b.WriteString(pathPrefix)
	for _, span := range spans {
		b.WriteString(span.text)
	}
	return b.String()
}

func (i *CollectionNameInflector) singularToken(token string) string {
	token = strings.TrimSpace(strings.ToLower(token))
	if token == "" {
		return token
	}
	if alias := i.aliasToken(token); alias != token {
		token = alias
	}

	i.mu.RLock()
	if _, ok := i.uncountable[token]; ok {
		i.mu.RUnlock()
		return token
	}
	if singular, ok := i.reverseIrreg[token]; ok {
		i.mu.RUnlock()
		return singular
	}
	if _, ok := i.irregular[token]; ok {
		i.mu.RUnlock()
		return token
	}
	i.mu.RUnlock()

	return singularTokenByRule(token)
}

func (i *CollectionNameInflector) pluralToken(token string) string {
	token = strings.TrimSpace(strings.ToLower(token))
	if token == "" {
		return token
	}
	if alias := i.aliasToken(token); alias != token {
		token = alias
	}

	i.mu.RLock()
	if _, ok := i.uncountable[token]; ok {
		i.mu.RUnlock()
		return token
	}
	if plural, ok := i.irregular[token]; ok {
		i.mu.RUnlock()
		return plural
	}
	if _, ok := i.reverseIrreg[token]; ok {
		i.mu.RUnlock()
		return token
	}
	i.mu.RUnlock()

	return pluralTokenByRule(token)
}

func (i *CollectionNameInflector) aliasToken(token string) string {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return token
	}

	i.mu.RLock()
	v, ok := i.aliases[token]
	i.mu.RUnlock()
	if ok {
		return v
	}
	return token
}

func (i *CollectionNameInflector) addAlias(from, to string) {
	from = strings.ToLower(strings.TrimSpace(from))
	to = strings.ToLower(strings.TrimSpace(to))
	if from == "" || to == "" {
		return
	}
	i.aliases[from] = to
	i.clearCachesLocked()
}

func (i *CollectionNameInflector) addIrregular(singular, plural string) {
	singular = strings.ToLower(strings.TrimSpace(singular))
	plural = strings.ToLower(strings.TrimSpace(plural))
	if singular == "" || plural == "" {
		return
	}
	i.irregular[singular] = plural
	i.reverseIrreg[plural] = singular
	i.clearCachesLocked()
}

func (i *CollectionNameInflector) addUncountable(name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return
	}
	i.uncountable[name] = struct{}{}
	i.clearCachesLocked()
}

func (i *CollectionNameInflector) addAcronym(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	i.acronyms[strings.ToLower(name)] = name
	i.clearCachesLocked()
}

func (i *CollectionNameInflector) clearCachesLocked() {
	i.singularCache = make(map[string]string, 512)
	i.pluralCache = make(map[string]string, 512)
	i.keyCache = make(map[string]string, 512)
}

func singularTokenByRule(token string) string {
	lower := strings.ToLower(strings.TrimSpace(token))
	if lower == "" || len(lower) <= 1 {
		return lower
	}

	hasSuffix := func(s string) bool { return strings.HasSuffix(lower, s) }

	switch {
	case hasSuffix("ies") && len(lower) > 3:
		// policies -> policy, properties -> property
		return lower[:len(lower)-3] + "y"

	case hasSuffix("ves") && len(lower) > 3:
		// conservative default:
		// leaves/lives are irregular above; for custom words, prefer f.
		return lower[:len(lower)-3] + "f"

	case hasSuffix("men") && len(lower) > 3:
		return lower[:len(lower)-3] + "man"

	case hasSuffix("children"):
		return lower[:len(lower)-3]

	case hasSuffix("matrices"):
		return lower[:len(lower)-3] + "x"

	case hasSuffix("vertices"):
		return lower[:len(lower)-3] + "x"

	case hasSuffix("indices"):
		return lower[:len(lower)-4] + "ex"

	case hasSuffix("analyses"):
		return lower[:len(lower)-2] + "is"

	case hasSuffix("theses"):
		return lower[:len(lower)-2] + "is"

	case hasSuffix("diagnoses"):
		return lower[:len(lower)-2] + "is"

	case hasSuffix("statuses"):
		return lower[:len(lower)-2]

	case hasSuffix("sses"):
		// classes -> class
		return lower[:len(lower)-2]

	case hasSuffix("ches") || hasSuffix("shes") || hasSuffix("xes") || hasSuffix("zes"):
		// branches -> branch, boxes -> box
		return lower[:len(lower)-2]

	case hasSuffix("oes") && len(lower) > 3:
		// heroes -> hero
		return lower[:len(lower)-2]

	case hasSuffix("ses") && len(lower) > 3:
		// cases -> case, phases -> phase
		return lower[:len(lower)-1]

	case hasSuffix("s") && !hasSuffix("ss"):
		return lower[:len(lower)-1]

	default:
		return lower
	}
}

func pluralTokenByRule(token string) string {
	lower := strings.ToLower(strings.TrimSpace(token))
	if lower == "" {
		return lower
	}

	hasSuffix := func(s string) bool { return strings.HasSuffix(lower, s) }

	switch {
	case hasSuffix("s"):
		return lower

	case hasSuffix("is") && len(lower) > 2:
		// analysis -> analyses, thesis -> theses
		return lower[:len(lower)-2] + "es"

	case hasSuffix("ex") && len(lower) > 2:
		// index -> indices is irregular above; default indexes is less surprising
		return lower + "es"

	case hasSuffix("ix") && len(lower) > 2:
		return lower + "es"

	case hasSuffix("fe") && len(lower) > 2:
		// life/wife are irregular above; default safe English-ish rule
		return lower[:len(lower)-2] + "ves"

	case hasSuffix("f") && len(lower) > 1:
		// leaf/wolf are irregular above; this handles common custom names
		return lower[:len(lower)-1] + "ves"

	case hasSuffix("y") && len(lower) > 1 && isConsonantASCII(lower[len(lower)-2]):
		return lower[:len(lower)-1] + "ies"

	case hasSuffix("ch") || hasSuffix("sh") || hasSuffix("x") || hasSuffix("z"):
		return lower + "es"

	case hasSuffix("o") && len(lower) > 1 && isConsonantASCII(lower[len(lower)-2]):
		return lower + "es"

	default:
		return lower + "s"
	}
}

func isConsonantASCII(b byte) bool {
	if b < 'a' || b > 'z' {
		return false
	}
	switch b {
	case 'a', 'e', 'i', 'o', 'u':
		return false
	default:
		return true
	}
}

type nameSpanKind uint8

const (
	spanToken nameSpanKind = iota + 1
	spanSeparator
)

type nameSpan struct {
	kind nameSpanKind
	text string
}

func scanNameSpans(name string) []nameSpan {
	if name == "" {
		return nil
	}

	var spans []nameSpan
	var b strings.Builder
	var currentKind nameSpanKind

	flush := func() {
		if b.Len() == 0 {
			return
		}
		spans = append(spans, nameSpan{kind: currentKind, text: b.String()})
		b.Reset()
	}

	for _, r := range name {
		kind := spanToken
		if isNameSeparator(r) {
			kind = spanSeparator
		}
		if currentKind == 0 {
			currentKind = kind
		}
		if kind != currentKind {
			flush()
			currentKind = kind
		}
		b.WriteRune(r)
	}
	flush()

	return spans
}

func splitNameTokens(name string) []string {
	spans := scanNameSpans(name)
	if len(spans) == 0 {
		return nil
	}

	var out []string
	for _, span := range spans {
		if span.kind != spanToken {
			continue
		}
		out = append(out, splitIdentifierToken(span.text)...)
	}
	return out
}

func splitIdentifierToken(token string) []string {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}

	runes := []rune(token)
	if len(runes) == 0 {
		return nil
	}

	var parts []string
	start := 0

	for idx := 1; idx < len(runes); idx++ {
		prev := runes[idx-1]
		cur := runes[idx]

		nextIsLower := false
		if idx+1 < len(runes) {
			nextIsLower = unicode.IsLower(runes[idx+1])
		}

		boundary := false

		switch {
		case unicode.IsDigit(prev) && unicode.IsLetter(cur):
			boundary = true
		case unicode.IsLetter(prev) && unicode.IsDigit(cur):
			boundary = true
		case unicode.IsLower(prev) && unicode.IsUpper(cur):
			boundary = true
		case unicode.IsUpper(prev) && unicode.IsUpper(cur) && nextIsLower:
			// APIKey -> API + Key, HTTPRoute -> HTTP + Route
			boundary = true
		}

		if boundary {
			parts = append(parts, string(runes[start:idx]))
			start = idx
		}
	}

	parts = append(parts, string(runes[start:]))
	return parts
}

func splitPath(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return []string{""}
	}

	var parts []string
	start := 0
	for idx, r := range name {
		if r == '.' {
			parts = append(parts, name[start:idx])
			start = idx + 1
		}
	}
	parts = append(parts, name[start:])
	return parts
}

func splitLastPath(name string) (prefix, leaf string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}

	lastDot := strings.LastIndexByte(name, '.')
	if lastDot < 0 {
		return "", name
	}
	return name[:lastDot+1], name[lastDot+1:]
}

func isNameSeparator(r rune) bool {
	switch r {
	case '_', '-', ' ', '\t', '\n', '\r', '/', ':':
		return true
	default:
		return false
	}
}

func applyTokenStyle(original, normalized string) string {
	if original == "" || normalized == "" {
		return normalized
	}

	if isAllUpper(original) {
		return strings.ToUpper(normalized)
	}

	if isAllLower(original) {
		return strings.ToLower(normalized)
	}

	if isTitleLike(original) {
		return upperFirstRune(strings.ToLower(normalized))
	}

	// Mixed/camel case. Preserve the normalized token in lower/title form based
	// on the first rune. This avoids turning APIKeys into apikeys.
	first, _ := utf8.DecodeRuneInString(original)
	if unicode.IsUpper(first) {
		return upperFirstRune(strings.ToLower(normalized))
	}
	return strings.ToLower(normalized)
}

func applySimpleCase(original, normalized string) string {
	return applyTokenStyle(original, normalized)
}

func isAllUpper(s string) bool {
	hasLetter := false
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		hasLetter = true
		if !unicode.IsUpper(r) {
			return false
		}
	}
	return hasLetter
}

func isAllLower(s string) bool {
	hasLetter := false
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		hasLetter = true
		if !unicode.IsLower(r) {
			return false
		}
	}
	return hasLetter
}

func isTitleLike(s string) bool {
	if s == "" {
		return false
	}
	first, size := utf8.DecodeRuneInString(s)
	if first == utf8.RuneError && size == 0 {
		return false
	}
	if !unicode.IsUpper(first) {
		return false
	}
	rest := s[size:]
	for _, r := range rest {
		if unicode.IsLetter(r) && !unicode.IsLower(r) {
			return false
		}
	}
	return true
}

func upperFirstRune(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && size == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
