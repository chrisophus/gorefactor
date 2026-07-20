package main

// Shared AST / type / package-loading helpers now live in internal/goload so
// the importable refactoring engines can use them without depending on package
// main. These aliases preserve the historical spellings used throughout the CLI.

import "github.com/chrisophus/gorefactor/internal/goload"

var (
	loadTypedPackages  = goload.LoadTypedPackages
	lookupNamedType    = goload.LookupNamedType
	findFileInPackages = goload.FindFileInPackages
	qualifierFor       = goload.QualifierFor
	signatureText      = goload.SignatureText
	validateGoSnippet  = goload.ValidateGoSnippet
	parseFuncLocator   = goload.ParseFuncLocator
	parseLocatorParts  = goload.ParseLocatorParts
	validateFuncTarget = goload.ValidateFuncTarget
	declNames          = goload.DeclNames
	receiverTypeName   = goload.ReceiverTypeName
)
