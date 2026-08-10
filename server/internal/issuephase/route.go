// Package issuephase holds the shape of an issue's route, shared by the
// service that seeds it at creation and the handler that edits it afterwards.
package issuephase

// PositionStep leaves room to insert a station between two others without
// renumbering their neighbours. Same convention as issue.position.
const PositionStep = 1000

// DefaultRoute is the route every new issue starts with.
//
// The same five stations whatever the issue is about — a requirement, a design,
// an implementation all get specified, reviewed, built, checked and closed.
// That sameness is what makes a route a property of an issue rather than a
// process for one kind of work, and it is why seeding it at creation is
// reasonable at all: a route that differed per issue could not have a default.
//
// They deliberately name the two reviews SEPARATELY. 方案评审 and 代码评审 ask
// different questions of different artifacts — "is this the right thing to
// build" versus "is this built right" — and a single 评审 station collapsed
// both into one pile, which is exactly the sorting a phase exists to do.
//
// They overlap with `status` at the ends (需求梳理 ≈ in_progress, 需求冻结 ≈
// done) and on their own would add nothing. What they add is ROUNDS: review
// happens more than once, status forgets every round but the current one, and a
// station per round keeps what each one asked for. A second pass appends as
// 方案评审 2 / 代码评审 2.
//
// packages/views/issues/components/phase-track.tsx carries the same list for
// its "apply template" menu. Duplicated rather than shared because the two live
// on opposite sides of the API and cannot import each other; the menu stays
// because a route can be deleted and someone has to be able to put it back.
var DefaultRoute = []string{"需求梳理", "方案评审", "代码评审", "测试验收", "需求冻结"}
