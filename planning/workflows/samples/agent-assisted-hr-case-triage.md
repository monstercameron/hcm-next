# Sample: Agent-Assisted HR Case Triage

Purpose: demonstrate a safely bounded `AGENT` step. The agent classifies and
summarizes; deterministic services decide routing and humans own sensitive
decisions.

```text
TASK collect HR request
  -> CAPABILITY classify/redact/quarantine attachments
  -> AGENT propose case type, urgency and missing questions
  -> CAPABILITY validate agent output and citations
  -> RULE apply deterministic routing/mandatory-escalation rules
  -> DECISION high-risk or uncertain?
       yes -> TASK specialist triage
       no  -> CAPABILITY create case and queue assignment
  -> END case-created
```

| Node            | Type       | Contract                                                                                              |
| --------------- | ---------- | ----------------------------------------------------------------------------------------------------- |
| intake          | TASK       | typed request, purpose, preferred contact and attachment refs                                         |
| quarantine      | CAPABILITY | malware/DLP/content-trust result; unsafe content never enters prompt                                  |
| propose_triage  | AGENT      | minimum-necessary redacted facts; output `TriageProposal/v1`; read-only tools only                    |
| validate_output | CAPABILITY | schema, allowed taxonomy, citations, unsupported-claim and policy checks                              |
| routing_rules   | RULE       | case-type/urgency/escalation decision table, including protected keywords as deterministic indicators |
| route           | DECISION   | specialist_required / standard_queue / unknown                                                        |
| specialist      | TASK       | human verifies/revises proposal and supplies typed result                                             |
| create_case     | CAPABILITY | creates case/compartment/assignment using deterministic validated result                              |
| end             | END        | case reference, routing evidence and agent provenance; never employment decision                      |

Required AGENT failure routes:

```text
prompt injection or hostile attachment -> quarantine + specialist task
model/provider ineligible             -> deterministic/manual route
timeout/budget exhausted              -> specialist task
invalid/ungrounded output             -> reject output + specialist task
tool request outside allowlist        -> deny + AI security incident policy
kill switch active                    -> bypass agent; deterministic/manual route
```

The client cannot submit system/tool messages. The server constructs agent history
and supplies opaque authorized references. Prompt/output content is excluded from
telemetry by default.
