package changectx

// promptFragment is the Go half of a review prompt. The consumer owns the
// harness half (the output schema, the rule against restating prior findings,
// that zero findings is a valid answer) and concatenates this as data. It says
// how Go code is conventionally read, so keep it about idiom and leave
// anything about output format out of it.
const promptFragment = `Reviewing Go.

Errors are values and the wrapping chain is the contract. A function that adds
context to an error wraps it with %w when a caller may need errors.Is or
errors.As to match it, and formats with %v when the chain should stop there.
An error is handled once: code that logs it and returns it has handled it
twice, and the caller reports it again. A dropped error is a finding unless the
code says why.

context.Context is the first parameter, named ctx, and travels down every call
that can block. Look for a function that takes one and ignores it, for
context.Background() created deep inside a request path, and for a context
kept in a struct field. Cancellation only works when the value carrying it
reaches the blocking call.

Every goroutine needs a defined end. Ask what stops it and what happens when
the caller returns first. A goroutine started per request with no cancellation
is a leak. A WaitGroup whose Add runs inside the goroutine is a race. Anything
a closure captures and two goroutines write needs a mutex or a channel.

Nil is legal for slices, maps, channels, function values, pointers, and
interfaces, and it behaves differently for each. Reading a nil map works and
writing one panics. A nil pointer inside a non-nil interface compares unequal
to nil, which is how a returned error becomes non-nil by accident.

Interfaces belong to the consumer. A new interface declared beside its only
implementation usually belongs in the package that calls it. Satisfaction is
checked at compile time, so a method added only to satisfy an interface is
either used or dead.

Tests are table-driven by convention: a slice of named cases with a subtest per
case under t.Run. Look for state shared between cases, for t.Parallel over a
captured loop variable, and for assertions on error strings where errors.Is
says what was meant.

Load-bearing idioms: defer for cleanup that must run on every path, a zero
value that is usable without construction, and early returns over nesting.
`
