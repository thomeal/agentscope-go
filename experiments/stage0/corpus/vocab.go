package corpus

import "fmt"

// systemPrompt is shared by every session and benchmark item. It carries no
// session marker and says nothing about context handling.
const systemPrompt = "You are an operations troubleshooting assistant helping a user review a long-running platform incident. All systems, records and codes here are synthetic. The user will ask you to read records with the record tool you have, or to make decisions by applying a rule they give you. Follow their instructions about what to include in your reply, and keep replies short."

// codeAlphabet leaves out characters that are easy to misread (0, O, 1, I, L),
// so a wrong answer reflects memory and not a misread glyph.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// kinds and subjects combine into labels: "<kind> for the <subject>". Each pair
// is used at most once in a session, so a label identifies its code on its own.
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
	"cache tier", "auth gateway", "notification service", "ingest pipeline",
	"analytics warehouse", "object store", "message broker", "scheduler service",
	"edge proxy", "api gateway", "session store", "metadata service",
	"inventory service", "pricing engine", "checkout frontend", "recommendation service",
	"fraud detector", "email relay", "sms gateway", "video transcoder",
	"image resizer", "geo resolver", "feature store", "model registry",
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

// decisionRules are deterministic and simple enough for a small model to apply.
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

type plainQuestionPair struct{ q, a string }

// plainQuestions introduce no facts: they make local turns ordinary without
// adding anything to score.
var plainQuestions = []plainQuestionPair{
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

// The question forms. F6 questions use exactly these, so nothing in a question's
// wording tells it apart from one that has an answer.
func qRequired(kind, subject string) string {
	return fmt.Sprintf("Which %s did the user require for the %s?", kind, subject)
}

func qChosen(kind, subject string) string {
	return fmt.Sprintf("Which %s was chosen in the decision about the %s?", kind, subject)
}

func qCurrent(kind, subject string) string {
	return fmt.Sprintf("What is the current %s for the %s?", kind, subject)
}

func qRecord(kind, id, subject string) string {
	return fmt.Sprintf("What %s does record %s give for the %s?", kind, id, subject)
}

func qLocalRecord(kind, id, subject string) string {
	return fmt.Sprintf("What %s does local record %s give for the %s?", kind, id, subject)
}

// refLine is the one labelled form of a code in a record.
func refLine(kind, subject, code string) string {
	return capitalize(kind) + " for the " + subject + ": " + code
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
