package bcl

import "strings"

// Diagnostic codes identify a class of problem independently of its wording.
// Tooling needs that: a CI job suppressing one rule, an editor offering a fix, a
// document linking to an explanation. Message text is free to improve; a code
// means the same thing forever, and a retired code is never reused.
//
// The ranges group by the stage that reports the problem:
//
//	BCL0001-0099  lexing and parsing
//	BCL0100-0199  references and declarations
//	BCL0200-0299  schemas and types
//	BCL0300-0399  expressions
//	BCL0400-0499  capabilities and policy
//	BCL0500-0599  modules and lockfiles
//	BCL0600-0699  decisions
//	BCL0700-0799  conventions and hygiene
const (
	CodeSyntaxDeclaration   = "BCL0001" // a statement did not start with a name, block, or declaration
	CodeSyntaxValue         = "BCL0002" // a field name was not followed by a value
	CodeUnterminatedLiteral = "BCL0003" // a string, heredoc, or block comment was never closed
	CodeNestingTooDeep      = "BCL0010" // the document nests deeper than MaxNestingDepth

	CodeDuplicateDeclaration = "BCL0100" // two declarations share a name
	CodeUnknownReference     = "BCL0101" // a reference names something that is not declared
	CodeUnknownPredicate     = "BCL0102" // a predicate reference has no matching predicate
	CodeCyclicReference      = "BCL0103" // declarations reference each other in a cycle

	CodeMissingRequiredField = "BCL0200" // a schema requires a field the document omits
	CodeUnknownField         = "BCL0201" // a field is not declared by the block's schema
	CodeTypeMismatch         = "BCL0202" // a value does not match its declared type
	CodeConstraintViolation  = "BCL0203" // a value breaks a schema constraint such as min or pattern

	CodeInvalidExpression = "BCL0300" // an expression does not compile
	CodeMatchNoCatchAll   = "BCL0301" // a match expression can fall through with no case

	CodeCapabilityRequired = "BCL0400" // the document needs a capability the caller did not grant
	CodeMissingEnv         = "BCL0401" // a required environment variable is not set
	CodePolicyRefused      = "BCL0402" // a host, path, or adapter is outside what the caller allows

	CodeModuleSource      = "BCL0500" // a module declares neither a source nor an inline body
	CodeMissingLockEntry  = "BCL0501" // a module is not recorded in the lockfile
	CodeChecksumMismatch  = "BCL0502" // a fetched module does not match its recorded checksum
	CodeModuleInputs      = "BCL0503" // a module invocation does not satisfy its parameters
	CodeMissingImportFile = "BCL0504" // an imported path does not exist

	CodeDecisionEffect    = "BCL0600" // a decision uses an effect its schema does not declare
	CodeDecisionStrategy  = "BCL0601" // a decision uses an unknown strategy or hit policy
	CodeDecisionDuplicate = "BCL0602" // a decision declares the same rule twice
	CodeDecisionConflict  = "BCL0603" // rules disagree for equivalent conditions

	CodeMissingVersion = "BCL0700" // the document declares no bcl version
	CodeUnused         = "BCL0701" // a constant, set, or step is declared but never used
	CodeDeprecated     = "BCL0702" // a declaration is marked deprecated
)

// diagnosticRule classifies a diagnostic by its message and carries the help text
// shown alongside it. Keeping both in one table means a new diagnostic gets a
// code and a hint together, or neither - they never drift apart.
type diagnosticRule struct {
	code  string
	hint  string
	match []string // the message must contain every entry
}

var diagnosticRules = []diagnosticRule{
	{CodeSyntaxDeclaration, "Start statements with a name, for example `field \"value\"` or `block \"id\" { ... }`.", []string{"expected declaration"}},
	{CodeSyntaxValue, "Add a scalar value, list, object, reference, or function call after the field name.", []string{"expected value"}},
	{CodeNestingTooDeep, "Flatten the document: blocks, lists and calls may nest up to 512 levels.", []string{"nesting is deeper"}},
	{CodeUnterminatedLiteral, "Close the string with the same quote style that opened it.", []string{"unterminated string"}},
	{CodeUnterminatedLiteral, "Close the multiline string with triple double quotes: `\"\"\"`.", []string{"unterminated multiline string"}},
	{CodeUnterminatedLiteral, "Close the raw string with a backtick.", []string{"unterminated raw string"}},
	{CodeUnterminatedLiteral, "End the heredoc with its marker on a line by itself.", []string{"unterminated heredoc"}},
	{CodeUnterminatedLiteral, "Close the block comment with `*/`.", []string{"unterminated block comment"}},

	{CodeCapabilityRequired, "Enable environment access with Options.AllowEnv or the CLI `--allow-env` flag.", []string{"env function requires AllowEnv"}},
	{CodeCapabilityRequired, "Enable time access with Options.AllowTime.", []string{"requires time capability"}},
	{CodeCapabilityRequired, "Enable hashing with Options.AllowHash.", []string{"requires hash capability"}},
	{CodeCapabilityRequired, "Enable encoding with Options.AllowEncoding.", []string{"requires encoding capability"}},
	{CodeMissingEnv, "Set the environment variable, provide an env file, or use `env(\"KEY\", default)`.", []string{"required env", "is not set"}},
	{CodePolicyRefused, "List what the document may reach in Options.AllowedHTTPHosts, AllowedDatasetRoots, or AllowedDatasetAdapters.", []string{"is not allowed"}},
	{CodePolicyRefused, "List the directory in Options.AllowedDatasetRoots if the document should read it.", []string{"is outside"}},

	{CodeInvalidExpression, "Check operator spelling, balanced brackets, and whether function capabilities are enabled.", []string{"invalid expression"}},
	{CodeMatchNoCatchAll, "Add a `case ANY => ...` arm so every input is handled.", []string{"no catch-all case"}},

	{CodeUnknownPredicate, "Declare the predicate, or correct the name.", []string{"unknown predicate"}},
	{CodeUnknownReference, "Define the referenced block/constant/set, import it, or fix the reference path.", []string{"unknown reference"}},
	{CodeCyclicReference, "Break the cycle: one of these declarations must not depend on the others.", []string{"cycle"}},
	{CodeDuplicateDeclaration, "Rename one declaration or remove the duplicate definition.", []string{"duplicate"}},

	{CodeMissingRequiredField, "Add the field or define a schema default.", []string{"missing required field"}},
	{CodeMissingRequiredField, "Add the field, or make it optional in the schema.", []string{"requires"}},
	{CodeUnknownField, "Remove the field, or declare it in the schema.", []string{"unknown field"}},
	{CodeTypeMismatch, "Change the value to match the declared type.", []string{"must be"}},
	{CodeTypeMismatch, "Change the value to match the declared type.", []string{"should be"}},

	{CodeMissingLockEntry, "Run `bcl modules lock <file>` and commit the generated lockfile.", []string{"missing lock entry"}},
	{CodeChecksumMismatch, "Re-fetch the module, or update the lockfile if the change is expected.", []string{"checksum mismatch"}},
	{CodeModuleSource, "Give the module a `source`, or declare its contents inline.", []string{"module requires source"}},
	{CodeModuleInputs, "Pass the inputs the module declares as parameters.", []string{"module", "input"}},
	{CodeMissingImportFile, "Create the file, or correct the import path.", []string{"import"}},

	{CodeDecisionEffect, "Declare the effect in the decision schema, or use one that is declared.", []string{"effect", "not declared"}},
	{CodeDecisionStrategy, "Use one of: first_match, highest_priority, deny_overrides, allow_overrides, collect_all.", []string{"invalid strategy"}},
	{CodeDecisionDuplicate, "Give each rule in a decision a distinct id.", []string{"duplicate rule"}},
	{CodeDecisionConflict, "Narrow the conditions, or set priorities so one rule clearly wins.", []string{"conflicting effects"}},

	{CodeMissingVersion, "Add a `bcl { version \"1.0\" }` block at the top of the document.", []string{"missing bcl version"}},
	{CodeDeprecated, "Point callers at the replacement with `replaced_by`.", []string{"deprecated"}},
	{CodeUnused, "Remove it, or reference it where it was meant to be used.", []string{"unused"}},
	{CodeUnused, "Connect the step to the workflow, or remove it.", []string{"unreachable"}},
}

// classifyDiagnostic returns the code and help text for a message. The first
// matching rule wins, so more specific patterns are listed first.
func classifyDiagnostic(msg string) (code string, hint string) {
	lower := strings.ToLower(msg)
	for _, rule := range diagnosticRules {
		matched := true
		for _, needle := range rule.match {
			if !strings.Contains(lower, needle) {
				matched = false
				break
			}
		}
		if matched {
			return rule.code, rule.hint
		}
	}
	return "", ""
}

// DiagnosticCode returns the stable code for a diagnostic message, or "" when the
// message is not yet classified.
func DiagnosticCode(msg string) string {
	code, _ := classifyDiagnostic(msg)
	return code
}

// applyDiagnosticCodes fills in the code of every diagnostic that does not carry
// one. Diagnostics are built in hundreds of places; classifying them once on the
// way out keeps the codes consistent without threading a code through each site,
// while still letting a site set its own.
func applyDiagnosticCodes(diags []Diagnostic) []Diagnostic {
	for i := range diags {
		if diags[i].Code == "" {
			diags[i].Code, _ = classifyDiagnostic(diags[i].Message)
		}
	}
	return diags
}
