package s3

import "fmt"

// systemPrompt is shared by every item. It says nothing about routing, sides or
// what kind of question comes next.
const systemPrompt = "You are an operations assistant helping a user review a long-running platform incident. All systems, records and codes here are synthetic. Use the record tool you have when a question needs a record, and keep replies short."

// answerPrompt is appended to Query when scoring. It is one fixed string, so it
// cannot differ by kind.
const answerPrompt = "Reply with only the answer: the exact code or the integer, or UNKNOWN if you cannot determine it. Use no other words."

// codeAlphabet leaves out characters that are easy to misread (0, O, 1, I, L),
// so a wrong answer reflects missing knowledge and not a misread glyph.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// kinds and subjects combine into labels: "<kind> for the <subject>". Each label
// is used at most once in an item, so a label identifies its code on its own.
// No entry contains another. Some subjects sound local on purpose, so that a word
// like "edge" or "site" in a query says nothing about where the answer lives.
var kinds = []string{
	"rollback token", "escalation bridge", "change ticket", "maintenance window code",
	"audit reference", "drain approval", "failover ticket", "capacity lease",
	"hotfix tag", "snapshot handle", "quarantine tag", "replay cursor",
	"access grant", "vendor case", "pager route", "certificate serial",
	"quota override", "schema lock", "traffic shift order", "dns hold",
	"backup manifest", "restore point", "canary gate", "deploy freeze",
	"incident bridge", "postmortem reference", "runbook revision", "config epoch",
	"flag batch", "key rotation ticket", "firewall exception", "load test permit",
	"patch bundle", "kernel pin", "image alias", "license reference",
	"billing hold", "retention waiver", "compliance exception", "shard map version",
	"replica promotion order", "index rebuild ticket", "queue drain token", "breaker override",
	"rate limit exemption", "tls renewal order", "path reservation", "migration batch",
	"cordon order", "upgrade slot", "power feed ticket", "rack move order",
	"spare part request", "shift swap ticket", "customer notice reference", "status draft",
	"forensics hold", "log export permit", "backfill job", "silence window",
}

var subjects = []string{
	"payments service", "search service", "billing cluster", "ledger service",
	"local cache tier", "auth gateway", "notification service", "ingest pipeline",
	"analytics warehouse", "object store", "message broker", "scheduler service",
	"edge proxy", "api gateway", "session store", "metadata service",
	"inventory service", "pricing engine", "checkout frontend", "recommendation service",
	"fraud detector", "email relay", "sms gateway", "device fleet",
	"site router", "branch gateway", "feature store", "model registry",
	"config service", "secrets vault", "audit pipeline", "log aggregator",
	"metrics backend", "tracing collector", "dns resolver", "load balancer pool",
	"vpn concentrator", "bastion pool", "backup controller", "batch runner",
	"etl orchestrator", "reporting service", "customer portal", "webhook relay",
	"mobile backend", "warehouse sync", "shipping tracker", "returns service",
	"loyalty service", "tax calculator", "invoice generator", "document store",
	"graph store", "time series store", "stream processor", "rules engine",
	"identity provider", "access proxy", "container registry", "build farm",
}

var cloudRecordTypes = []string{
	"on-call handover notes", "incident timeline extract", "change log extract",
	"capacity review notes", "vendor correspondence digest", "service health digest",
	"post-deploy checklist results",
}

var localRecordTypes = []string{
	"edge node diagnostics", "site inventory notes", "device maintenance log",
	"branch gateway journal",
}

// fillers are code-free log lines. They give records realistic bulk without
// adding anything a question could ask about.
var fillers = []func(r *prng, subject string) string{
	func(r *prng, s string) string {
		return fmt.Sprintf("Node node-%d of the %s reached %d percent heap, collection pause %d ms, no alert raised.", r.between(1, 99), s, r.between(40, 92), r.between(20, 480))
	},
	func(r *prng, s string) string {
		return fmt.Sprintf("Operator note: traffic on the %s stayed inside its normal band for %d minutes.", s, r.between(5, 90))
	},
	func(r *prng, s string) string {
		n := r.between(40, 200)
		return fmt.Sprintf("Probe against the %s returned %d of %d healthy responses from zone %c.", s, n-r.between(0, 3), n, 'a'+rune(r.intn(4)))
	},
	func(r *prng, s string) string {
		return fmt.Sprintf("Retry budget of the %s dropped to %d percent, then recovered without paging anyone.", s, r.between(55, 95))
	},
	func(r *prng, s string) string {
		return fmt.Sprintf("Deploy pipeline skipped the %s because no artifact changed in the last %d hours.", s, r.between(2, 48))
	},
	func(r *prng, s string) string {
		return fmt.Sprintf("Queue depth on the %s peaked at %d messages and drained within %d seconds.", s, r.between(100, 9000), r.between(3, 240))
	},
	func(r *prng, s string) string {
		return fmt.Sprintf("Disk usage on the %s volume reached %d percent, within the agreed limit.", s, r.between(50, 84))
	},
	func(r *prng, s string) string {
		return fmt.Sprintf("Reviewer comment: nothing on the %s dashboard needs a follow-up from this shift.", s)
	},
	func(r *prng, s string) string {
		a := r.between(40, 300)
		return fmt.Sprintf("Latency at the %s p99 moved from %d ms to %d ms after the routine restart.", s, a+r.between(5, 60), a)
	},
	func(r *prng, s string) string {
		return fmt.Sprintf("Synthetic check of the %s passed %d consecutive runs in zone %c.", s, r.between(6, 400), 'a'+rune(r.intn(4)))
	},
	func(r *prng, s string) string {
		return fmt.Sprintf("Connection pool of the %s held %d open sessions, %d idle, and no waiters.", s, r.between(30, 800), r.between(1, 29))
	},
	func(r *prng, s string) string {
		return fmt.Sprintf("Shift log: the %s owner confirmed the morning review finished %d minutes early.", s, r.between(2, 40))
	},
}

// readTemplates ask to read a record without restating it. Arguments: the record
// as "record R-417" or "local record L-283", then the tool name.
var readTemplates = []string{
	"Please read %[1]s with %[2]s. Do not restate its contents in your reply; just confirm you have read it. I will ask for details later.",
	"Use %[2]s on %[1]s. Just confirm you have read it and keep the details out of your reply for now; I will ask later.",
	"Next, pull %[1]s with %[2]s. No need to repeat anything from it, a one-line confirmation is enough. I will ask for details later.",
}

// decisionRules are simple enough for a small model to apply. They only add
// published noise to a history; no query asks about one.
var decisionRules = []struct {
	attr    string
	highest bool
	lo, hi  int
}{
	{"priority score", true, 1, 99},
	{"error rate in basis points", false, 1, 400},
	{"spare capacity percent", true, 5, 95},
	{"age in days", false, 1, 200},
}

type plainExchange struct{ q, a string }

// plainExchanges introduce no codes: they make a history ordinary without adding
// anything to answer.
var plainExchanges = []plainExchange{
	{"Quick question, no tool needed: in one sentence, why do we drain a node before patching it?", "Draining lets in-flight work finish so patching does not drop requests."},
	{"No tool needed: in one sentence, what is the main risk of restarting a cache during peak traffic?", "A cold cache can push a sudden load spike onto the systems behind it."},
	{"One sentence, no tool: why do we keep a way back ready during a deploy?", "It lets us restore the last good state quickly if the deploy misbehaves."},
	{"One sentence, no tool: what does a growing queue usually tell you about its consumers?", "Consumers are falling behind the rate at which work arrives."},
	{"No tool needed: in one sentence, why is alert silencing time-boxed?", "A time box stops a temporary silence from hiding a later real problem."},
	{"Quick one, no tool: why do we compare p99 latency and not only the average?", "The tail shows the slow requests that an average hides."},
	{"One sentence, no tool: why do we stage a change on a small slice of traffic first?", "A small slice limits the damage if the change is bad."},
	{"No tool needed: in one sentence, why should a failover be rehearsed?", "Rehearsal exposes broken steps before a real outage depends on them."},
	{"One sentence, no tool: what is the point of an incident timeline?", "It records the order of events so the review can find causes."},
	{"Quick one, no tool: why do we cap retries on a failing dependency?", "Uncapped retries add load and can prolong the failure."},
}

// Statement forms in published text. A statement in a user message and one in a
// reply take different shapes, so both places hold values in an ordinary way.
var userStatements = []string{
	"For the handover, the %[1]s for the %[2]s is %[3]s.",
	"Please keep this in view: the %[1]s for the %[2]s is %[3]s.",
	"Update from the bridge: the %[1]s for the %[2]s is %[3]s.",
	"Note for the review, the %[1]s for the %[2]s is %[3]s.",
}

var replyRequests = []string{
	"Please state the %[1]s for the %[2]s so I can paste it into the note.",
	"Quick check, which %[1]s do we hold for the %[2]s?",
	"Before we move on, confirm the %[1]s for the %[2]s.",
}

const clearedStatement = "The %[1]s for the %[2]s was revoked and nothing has replaced it, so treat it as UNKNOWN for now."

// Query forms. Arguments: kind, subject, then a record reference where one is
// named. No form says where the answer lives.
var plainForms = []string{
	"What is the %[1]s for the %[2]s?",
	"Which %[1]s applies to the %[2]s?",
	"Remind me of the %[1]s for the %[2]s.",
	"What %[1]s do we have for the %[2]s?",
	"I need the %[1]s for the %[2]s.",
	"Give me the %[1]s for the %[2]s.",
}

// recordWordForms mention a record without naming one. Their histories always
// hold the value in a record; the two halves of a pair differ in whether a recent
// message also states it.
var recordWordForms = []string{
	"What %[1]s did the record give for the %[2]s?",
	"Going by the record you read, what is the %[1]s for the %[2]s?",
	"Which %[1]s does the record show for the %[2]s?",
}

// longForms are long and simple: all that bulk asks for one value. Some borrow
// the words of a hard problem, so effort words do not separate them either.
var longForms = []string{
	"I am writing the shift handover for the next crew and the bridge call starts in a few minutes, " +
		"so I would rather not walk back through everything we looked at today, the capacity numbers, " +
		"the vendor notes or the deploy checklist. The template has a single blank field left and it is " +
		"the %[1]s for the %[2]s. Please tell me what goes in that field. I only need the value itself, " +
		"with no explanation of where it came from or how we got there, because the rest of the note " +
		"is already written and signed off by the reviewer.",
	"Before you answer, some background so you know why I am asking. The review board meets after lunch " +
		"and they have already seen the capacity numbers, the vendor correspondence and the deploy checklist, " +
		"and none of that needs to be repeated or reworked. What they asked me to confirm is narrower than " +
		"all of that. Take your time and think it through step by step if you have to, but the only thing " +
		"I need from you in the end is the %[1]s for the %[2]s.",
	"The customer liaison has asked me for one detail for the status update that goes out at the top of " +
		"the hour, and I want to make sure I hand over the right one and not something that merely looks " +
		"similar, since there are a lot of similar looking values flying around in this review. Could you " +
		"work out for me what the %[1]s for the %[2]s is? A single value is all I can use in the update.",
	"We have been going back and forth across several services today and I have lost track of some of " +
		"the smaller details. There is no need to reopen any of the earlier analysis, re-check any of the " +
		"dashboards, or propose next steps at this stage. I just want one small detail confirmed before " +
		"I close my notes for this shift: what is the %[1]s for the %[2]s?",
}

// namedForms name a record by id. The same text serves both sides, so only the
// id tells them apart.
var namedForms = []string{
	"What %[1]s does %[3]s give for the %[2]s?",
	"Check %[3]s: what is the %[1]s for the %[2]s?",
	"Look in %[3]s and tell me the %[1]s for the %[2]s.",
}

var namedLongForms = []string{
	"I am finishing my notes and the person who holds the keys to the cabinet leaves in a few minutes, " +
		"so I would rather not go through the whole history of today again. The one thing missing from " +
		"the notes is in %[3]s, so please pull it up and tell me the %[1]s for the %[2]s. I only need the " +
		"value itself, nothing about how the rest of it reads, because the note is otherwise complete.",
	"The review board asked me one narrow question and I do not want to reopen anything else we looked " +
		"at today, including the capacity numbers or the vendor notes, because those have already been " +
		"accepted. Take your time and check %[3]s carefully, then give me the %[1]s for the %[2]s and " +
		"nothing more, since the rest of the answer is already written up.",
}

func capitalize(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}

func article(word string) string {
	switch word[0] {
	case 'a', 'e', 'i', 'o', 'u':
		return "an"
	}
	return "a"
}
