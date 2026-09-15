# HandoffGuard

## Product Requirements Document

**Version:** 1.0
**Status:** MVP Specification
**Primary Language:** Go
**Product Category:** AI Infrastructure / Agent Security / Developer Infrastructure
**Target MVP:** 6–8 weeks
**Deployment:** Open-source self-hosted first

---

# 1. Product Summary

HandoffGuard is a security and policy enforcement layer for multi-agent AI systems.

Its purpose is to guarantee that when one AI agent delegates a task to another agent, the security constraints attached to the original task cannot silently disappear or become weaker.

The central invariant is:

> A child agent may receive equal or less authority than its parent agent, but it may never gain additional authority or drop mandatory obligations during a handoff.

Example:

A customer-support agent receives permission to process a refund with these restrictions:

* only access order `#48319`
* do not expose payment information
* refunds above $500 require manager approval
* may use `orders.read`
* may use `refund.create`
* may not modify customer identity

The support agent delegates the refund to a billing agent.

Without HandoffGuard, the billing agent might receive:

> "Please refund order 48319."

The original restrictions may disappear.

With HandoffGuard, the delegation carries a signed machine-readable policy envelope containing the restrictions.

The billing agent cannot drop them.

---

# 2. The Problem

Modern agent frameworks increasingly support:

* multiple specialized agents
* agent-to-agent handoffs
* MCP tools
* database access
* SaaS APIs
* cloud infrastructure access
* payments
* customer records
* autonomous workflows

However, authorization is usually evaluated at the individual agent or tool level.

This creates a delegation problem.

Consider:

```text
User
 ↓
Support Agent
 ↓
Billing Agent
 ↓
Payment Agent
 ↓
Stripe / Internal API
```

The original request may contain important restrictions.

```text
Refund this order.

Restrictions:
- maximum $500 without approval
- use data only for this customer-support request
- don't expose payment details
```

After several handoffs, the final agent might only receive:

```text
Issue refund.
```

The workflow therefore has authorization at individual points but no guaranteed continuity of authorization across the entire delegation chain.

This is similar to privilege propagation in distributed systems.

HandoffGuard turns constraints into structured state that travels with the workflow.

---

# 3. Product Vision

HandoffGuard should become:

> "The authorization inheritance layer for autonomous software agents."

Analogies:

```text
OAuth     → identity/access for APIs

OPA       → policy decisions

OpenTelemetry → distributed tracing

HandoffGuard → delegated authority and obligation propagation
```

It does not replace agent frameworks.

It integrates with them.

Supported ecosystem long term:

```text
OpenAI Agents
Google ADK
LangGraph
Claude / MCP
CrewAI
Semantic Kernel
Custom agents
```

---

# 4. Target Users

## Primary persona: AI Platform Engineer

Works at a company deploying multiple production agents.

Pain points:

* does not know what authority downstream agents inherit
* manually implements approval logic
* difficult to audit agent actions
* agent frameworks handle delegation differently
* security team is uncomfortable with autonomous actions

Needs:

* deterministic authorization
* SDK/framework integrations
* minimal latency
* auditability
* developer-friendly tooling

---

## Secondary persona: Security Engineer

Responsible for:

* least privilege
* service identities
* sensitive data access
* compliance
* production controls

Needs to answer:

```text
Why was this agent allowed to perform this action?

Which agent delegated the task?

What permissions did it inherit?

Did its permissions expand during delegation?

Was human approval required?

Which policy version authorized this?
```

---

## Third persona: Compliance / Governance Team

Needs an evidence trail showing:

```text
Agent A
 → Agent B
 → Agent C
 → tool action

Policy inherited: yes
Approval required: yes
Approval obtained: yes
Policy version: 8
Evidence integrity: valid
```

---

# 5. Product Goals

## G1 — Constraint inheritance

Security constraints must follow tasks across agent handoffs.

---

## G2 — Prevent authority expansion

A child agent must never receive more authority than the parent delegation allows.

For example:

```text
Parent:

orders.read
refund.create
```

Child attempts:

```text
orders.read
refund.create
customer.delete
```

Result:

```text
HANDOFF DENIED

Reason:
Child authority exceeds parent authority.

Added capability:
customer.delete
```

---

## G3 — Preserve mandatory obligations

A child may not remove requirements such as:

```text
manager approval
data residency
expiration
purpose restrictions
maximum delegation depth
resource restrictions
```

---

## G4 — Cryptographically verifiable delegation

Every delegation envelope should be signed.

Anyone inspecting the audit history should be able to verify that it was not modified.

---

## G5 — Framework independence

The policy model should not depend on one LLM provider.

---

## G6 — Deterministic enforcement

Authorization decisions must not depend on asking an LLM:

> "Does this seem allowed?"

Policy decisions must be deterministic.

---

# 6. Non-Goals

The MVP will NOT attempt to:

* detect every prompt injection attack
* replace IAM
* replace OAuth
* replace MCP authorization
* replace observability platforms
* evaluate model response quality
* moderate general user content
* automatically infer every policy using an LLM
* provide universal compliance certification
* support every agent framework

These would make the project too broad.

---

# 7. Core Concept: Obligation Envelope

Every task carries an **Obligation Envelope**.

Example:

```json
{
  "id": "env_01K4R92",
  "version": "1",

  "issuer": {
    "agent": "support-agent",
    "version": "2.3"
  },

  "recipient": {
    "agent": "billing-agent"
  },

  "purpose": "customer_support_refund",

  "resources": {
    "orders": ["48319"],
    "customers": ["cus_8291"]
  },

  "allowed_actions": [
    "orders.read",
    "refunds.create"
  ],

  "denied_actions": [
    "customers.delete",
    "customers.update_identity",
    "payments.export"
  ],

  "data_classes": [
    "customer_pii",
    "payment_metadata"
  ],

  "approvals": [
    {
      "condition": "refund.amount > 500",
      "required_role": "refund_manager"
    }
  ],

  "delegation": {
    "max_depth": 2,
    "current_depth": 1,
    "may_expand_authority": false
  },

  "expires_at": "2026-09-15T01:00:00Z",

  "policy_version": "refund-policy-v8",

  "parent_envelope": "env_01K4R10",

  "signature": "ed25519..."
}
```

---

# 8. Delegation Rules

HandoffGuard implements monotonic delegation.

A child can narrow a policy.

A child cannot broaden it.

## Allowed changes

```text
Shorter expiration

Parent:
expires in 60 minutes

Child:
expires in 20 minutes

✓ ALLOWED
```

```text
Fewer tools

Parent:
orders.read
refund.create
email.send

Child:
orders.read
refund.create

✓ ALLOWED
```

```text
More approval requirements

Parent:
approval > $500

Child:
approval > $300

✓ ALLOWED
```

```text
Narrower resources

Parent:
customer/*

Child:
customer/48319

✓ ALLOWED
```

---

## Forbidden changes

```text
More tools

Parent:
orders.read

Child:
orders.read
orders.delete

✗ DENIED
```

```text
Longer expiration

Parent:
30 min

Child:
24 hours

✗ DENIED
```

```text
Dropped approval

Parent:
refund > $500 requires manager

Child:
no approval

✗ DENIED
```

```text
Wider data access

Parent:
customer 48319

Child:
all customers

✗ DENIED
```

```text
More delegation

Parent:
max depth = 2

Child:
max depth = 5

✗ DENIED
```

---

# 9. Primary User Workflow

## Step 1 — User starts task

```text
"Refund order #48319 for $825."
```

The initial application creates:

```text
Root Obligation Envelope
```

---

## Step 2 — Support agent wants to delegate

Support Agent:

```text
delegate(
   billing-agent,
   refund order #48319
)
```

HandoffGuard receives:

```text
parent envelope
requested recipient
requested child permissions
```

---

## Step 3 — Policy engine evaluates delegation

```text
Parent
   ↓
Requested Child
   ↓
Policy Diff
   ↓
Monotonicity Evaluation
```

Result:

```text
ALLOW
```

or

```text
DENY
```

---

## Step 4 — Signed child envelope created

If allowed:

```text
env_parent
    ↓
env_child
```

The new envelope includes:

```text
parent envelope hash
issuer
recipient
policy version
constraints
signature
```

---

# 10. Tool Execution Workflow

Before an agent performs a consequential tool action:

```text
Billing Agent
      ↓
HandoffGuard Gateway
      ↓
Policy Engine
      ↓
Allowed?
   ↙       ↘
 Yes       No
 ↓          ↓
Tool      Block
```

Example:

```text
refund.create(
   order=48319,
   amount=825
)
```

Policy check finds:

```text
refund.amount > 500
```

Required:

```text
refund_manager approval
```

No approval exists.

Result:

```text
DENY

RULE:
refund.manager_approval

DETAIL:
Refunds greater than $500 require
refund_manager approval.
```

---

# 11. Human Approval

The MVP will include structured approvals.

Approval object:

```json
{
  "id": "approval_7832",

  "envelope_id": "env_01K4R92",

  "action": "refunds.create",

  "resource": "order:48319",

  "constraints": {
    "max_amount": 825
  },

  "approved_by": "manager_193",

  "role": "refund_manager",

  "expires_at": "2026-09-15T00:30:00Z",

  "status": "APPROVED"
}
```

Approval should be tied to:

* action
* resource
* envelope
* amount or critical parameters
* approver
* expiration

Avoid generic approvals such as:

```text
"Allow billing-agent everything for the next hour."
```

---

# 12. Functional Requirements

## FR-001 — Create Root Envelope

API must allow applications to create a signed root envelope.

```http
POST /v1/envelopes
```

---

## FR-002 — Validate Envelope

The system must verify:

* signature
* issuer
* expiration
* policy version
* revocation state

---

## FR-003 — Delegate Envelope

```http
POST /v1/envelopes/{id}/delegate
```

Input:

```json
{
  "recipient": "billing-agent",
  "constraints": {}
}
```

System evaluates whether the child is a valid narrowing.

---

## FR-004 — Detect Permission Broadening

The system must detect additions to:

* actions
* resources
* data classes
* delegation permissions

---

## FR-005 — Detect Obligation Removal

Must detect removal or weakening of:

* approvals
* expiration
* deny rules
* purpose
* data restrictions

---

## FR-006 — Tool Authorization

```http
POST /v1/evaluate/action
```

Example request:

```json
{
  "envelope_id": "env_01K4R92",
  "agent_id": "billing-agent",
  "tool": "refunds.create",
  "arguments": {
    "order_id": "48319",
    "amount": 825
  }
}
```

Response:

```json
{
  "decision": "DENY",
  "violations": [
    {
      "rule": "refund_manager_approval",
      "reason": "Refund exceeds $500"
    }
  ]
}
```

---

## FR-007 — Approval Registration

```http
POST /v1/approvals
```

---

## FR-008 — Envelope Revocation

Security administrator must be able to revoke an envelope.

```http
POST /v1/envelopes/{id}/revoke
```

All descendants become invalid.

Example:

```text
env1
 ├─ env2
 │   └─ env4
 └─ env3
```

Revoking:

```text
env2
```

invalidates:

```text
env2
env4
```

but not:

```text
env3
```

---

## FR-009 — Delegation Graph

System must reconstruct:

```text
User
 ↓
Support
 ↓
Billing
 ↓
Notification
```

with envelope IDs and action decisions.

---

## FR-010 — Audit Verification

CLI:

```bash
handoffguard audit verify run_8392
```

Output:

```text
Audit verification

12 events checked
12 signatures valid
0 missing parent references
0 modified envelopes

STATUS: VALID
```

---

# 13. CLI

The CLI is important because infrastructure engineers prefer tools they can immediately test.

Binary:

```bash
handoffguard
```

Commands:

```bash
handoffguard init

handoffguard policy validate

handoffguard policy diff

handoffguard envelope inspect

handoffguard audit verify

handoffguard test

handoffguard server

handoffguard gateway
```

Example:

```bash
handoffguard policy diff \
  policies/main.yaml \
  policies/proposed.yaml
```

Output:

```text
POLICY DIFF

Agent:
billing-agent

New capability:
+ customers.update

Removed requirement:
- manager approval above $500

Delegation depth:
2 → 5

RISK: CRITICAL

Policy broadens effective authority.
```

---

# 14. GitHub Integration

Create a GitHub Action.

Example:

```yaml
- uses: handoffguard/policy-check@v1
```

Pull request:

```text
HandoffGuard Security Check ❌

Authority expanded.

billing-agent

+ payments.refund
+ customer.update

Removed:
- manager approval > $500

Merge should be blocked.
```

This feature is especially valuable for the portfolio because recruiters immediately understand CI security gates.

---

# 15. Architecture

```text
                 ┌─────────────────┐
                 │ User / Event    │
                 └────────┬────────┘
                          │
                          ▼
                 ┌─────────────────┐
                 │ Agent A         │
                 └────────┬────────┘
                          │ handoff
                          ▼
              ┌────────────────────────┐
              │ HandoffGuard Gateway   │
              └───────────┬────────────┘
                          │
             ┌────────────┴─────────────┐
             │                          │
             ▼                          ▼
 ┌────────────────────┐       ┌────────────────────┐
 │ Delegation Engine  │       │ Policy Engine      │
 └──────────┬─────────┘       └─────────┬──────────┘
            │                           │
            └────────────┬──────────────┘
                         ▼
                ┌─────────────────┐
                │ Envelope Store  │
                │ PostgreSQL      │
                └────────┬────────┘
                         │
                         ▼
                 ┌────────────────┐
                 │ Agent B        │
                 └───────┬────────┘
                         │
                         ▼
              ┌──────────────────────┐
              │ Tool Gateway         │
              └──────────┬───────────┘
                         │
                   policy decision
                         │
                    ┌────┴────┐
                  allow      deny
                    │          │
                    ▼          ▼
                  MCP        Audit
                  Tool       Event
```

---

# 16. Technology Stack

## Backend

**Go 1.25+**

Why Go:

* excellent concurrency model
* common in cloud/platform tooling
* fast startup
* easy static binary distribution
* strong networking support
* strong resume signal for infrastructure roles

---

## HTTP

Use:

```text
chi
```

or standard `net/http`.

Avoid a huge web framework.

---

## Database

```text
PostgreSQL
```

Libraries:

```text
pgx
sqlc
```

Benefits:

* explicit SQL
* type-safe generated code
* easy deployment
* strong production pattern

---

## Policy expressions

Use:

```text
CEL — Common Expression Language
```

Example:

```text
refund.amount > 500
```

Instead of creating a custom arbitrary scripting language.

HandoffGuard itself handles authority monotonicity.

CEL handles conditions.

---

## Cryptography

Use:

```text
Ed25519
SHA-256
```

Canonicalize signed JSON before signing.

Envelope:

```text
Canonical JSON
      ↓
SHA-256
      ↓
Ed25519 signature
```

---

## Observability

Use:

```text
OpenTelemetry
```

Emit spans for:

```text
envelope.created
handoff.requested
handoff.allowed
handoff.denied
tool.allowed
tool.denied
approval.created
envelope.revoked
```

---

## Packaging

```text
Docker
Docker Compose
GitHub Actions
```

MVP should run with:

```bash
docker compose up
```

---

# 17. Recommended Repository Structure

```text
handoffguard/

cmd/
  server/
  gateway/
  cli/

internal/

  envelope/
    model.go
    signing.go
    validation.go

  policy/
    evaluator.go
    monotonicity.go
    expressions.go

  delegation/
    service.go
    graph.go

  approval/
    service.go

  gateway/
    middleware.go
    mcp.go

  audit/
    events.go
    chain.go
    verification.go

  storage/
    postgres/
    queries/

  telemetry/

api/
  openapi.yaml

policies/
  examples/

examples/
  refund-agent/
  vulnerable-agent/

migrations/

tests/
  integration/
  adversarial/

docker/

docs/
```

---

# 18. Database Model

## agents

```text
id
organization_id
name
version
framework
created_at
```

---

## envelopes

```text
id
parent_id
run_id

issuer_agent_id
recipient_agent_id

purpose

constraints_json

policy_version

expires_at

signature

revoked_at

created_at
```

---

## handoffs

```text
id
run_id

parent_envelope_id
child_envelope_id

from_agent
to_agent

decision

reason

created_at
```

---

## approvals

```text
id

envelope_id

action
resource

constraints_json

approver_id
approver_role

expires_at

status

created_at
```

---

## tool_actions

```text
id

run_id
agent_id
envelope_id

tool_name

arguments_hash

decision

reason

created_at
```

Do not store raw sensitive tool arguments by default.

Store:

```text
hash + selected metadata
```

---

## violations

```text
id

run_id

envelope_id

rule_id

severity

description

metadata_json

created_at
```

---

## audit_events

```text
id

run_id

event_type

payload_hash

previous_event_hash

timestamp
```

This creates a tamper-evident hash chain.

---

# 19. API

## Envelope Creation

```http
POST /v1/envelopes
```

---

## Delegate

```http
POST /v1/envelopes/{id}/delegate
```

---

## Evaluate Handoff

```http
POST /v1/evaluate/handoff
```

---

## Evaluate Action

```http
POST /v1/evaluate/action
```

---

## Create Approval

```http
POST /v1/approvals
```

---

## Revoke Envelope

```http
POST /v1/envelopes/{id}/revoke
```

---

## Retrieve Chain

```http
GET /v1/runs/{runId}/chain
```

---

## Retrieve Violations

```http
GET /v1/runs/{runId}/violations
```

---

## Policy Diff

```http
POST /v1/policies/diff
```

---

## Verify Audit

```http
POST /v1/audit/{runId}/verify
```

---

# 20. Developer SDK

Start with a Go SDK.

Example:

```go
client := handoffguard.NewClient(
    handoffguard.WithEndpoint("http://localhost:8080"),
)

result, err := client.AuthorizeAction(ctx, handoffguard.ActionRequest{
    EnvelopeID: envelope.ID,
    AgentID:    "billing-agent",
    Tool:       "refunds.create",
    Arguments: map[string]any{
        "order_id": "48319",
        "amount":   825,
    },
})
```

Response:

```go
if result.Decision == handoffguard.Deny {
    return fmt.Errorf(
        "HandoffGuard denied operation: %s",
        result.Reason,
    )
}
```

---

# 21. Agent Framework Integration

## First Integration

Build against one framework only.

Recommended:

```text
OpenAI Agents SDK + MCP
```

The product itself remains written in Go.

A tiny Python adapter can communicate with the Go gateway.

Example:

```text
Python Agent
     ↓ HTTP/gRPC
Go HandoffGuard
     ↓
MCP Tool
```

That actually strengthens the project because it demonstrates cross-language systems integration.

---

# 22. MCP Gateway

HandoffGuard should operate as a proxy:

```text
Agent

 ↓

HandoffGuard MCP Gateway

 ↓

Real MCP Server
```

Agent believes it is calling:

```text
refund.create
```

HandoffGuard intercepts it.

```text
1. Identify agent
2. Load envelope
3. Evaluate action
4. Check approval
5. Record audit event
6. Forward or deny
```

---

# 23. Required MVP Demo

Create a fake company called:

```text
AcmeShop
```

It has three agents:

```text
SupportAgent
BillingAgent
NotificationAgent
```

Available MCP tools:

```text
orders.get
refund.create
customer.update
email.send
payment.export
```

Policy:

```yaml
purpose: customer_support

actions:
  allow:
    - orders.get
    - refund.create
    - email.send

  deny:
    - customer.update
    - payment.export

refund:
  manager_approval_above: 500

delegation:
  max_depth: 2
  expand_authority: false
```

---

# 24. Demo Attack #1 — Dropped Approval

Parent:

```text
refund > $500 requires approval
```

Billing agent proposes child envelope without requirement.

Expected:

```text
HANDOFF DENIED

Violation:
MANDATORY_OBLIGATION_REMOVED

Removed:
refund_manager_approval
```

---

# 25. Demo Attack #2 — Privilege Expansion

Billing agent requests:

```text
payment.export
```

Parent lacks permission.

Expected:

```text
HANDOFF DENIED

AUTHORITY_EXPANSION

New capability:
payment.export
```

---

# 26. Demo Attack #3 — Scope Expansion

Parent:

```text
order = 48319
```

Child:

```text
order = *
```

Expected:

```text
DENIED

RESOURCE_SCOPE_EXPANSION
```

---

# 27. Demo Attack #4 — Expiration Expansion

Parent:

```text
expires = 15 minutes
```

Child:

```text
expires = 24 hours
```

Expected:

```text
DENIED

EXPIRATION_BROADENED
```

---

# 28. Demo Attack #5 — Too Much Delegation

Parent:

```text
max depth = 2
```

Agent attempts third delegation.

Expected:

```text
DENIED

MAX_DELEGATION_DEPTH_EXCEEDED
```

---

# 29. Dashboard

The UI should remain intentionally small.

Do not spend most development time building React screens.

## Overview

```text
Runs                     1,834

Agent Handoffs            6,943

Blocked Delegations          83

Blocked Tool Actions         219

Active Policies               12
```

---

## Run Detail

Display:

```text
Support Agent

    │ env_918
    │
    ▼

Billing Agent

    │ env_921
    │
    ▼

Notification Agent
```

Clicking an edge shows:

```text
Inherited constraints: 8

Narrowed: 2

Expanded: 0

Signature: VALID
```

---

# 30. Non-Functional Requirements

## Performance

Policy evaluation:

```text
p95 < 10 ms
```

Gateway overhead target:

```text
p95 < 25 ms
```

excluding external tool execution.

---

## Reliability

Gateway should fail closed for protected operations.

If HandoffGuard cannot determine authorization:

```text
DENY
```

rather than:

```text
ALLOW
```

---

## Scalability

MVP benchmark:

```text
1,000 authorization decisions / second
```

on a normal developer machine should be a stretch goal.

---

## Security

Must protect against:

* forged envelopes
* modified envelopes
* replayed envelopes
* expired envelopes
* revoked envelopes
* missing parent envelopes
* cyclic parent references
* malicious policy changes
* approval replay

---

# 31. Testing Strategy

Testing is one of the most valuable parts of this project.

Target:

```text
80%+ unit test coverage
```

but more importantly build adversarial tests.

Create a benchmark suite:

```text
100 valid delegation scenarios
100 invalid delegation scenarios
```

Categories:

```text
permission expansion
resource expansion
removed approval
expiry expansion
purpose change
delegation expansion
signature modification
revocation
approval replay
invalid policy
```

---

# 32. Property-Based Testing

Use Go property testing/fuzzing.

Important property:

For any valid parent policy `P` and child `C`:

```text
if C contains more effective authority than P
then authorize(P, C) must return DENY
```

Use Go fuzz tests:

```go
func FuzzDelegationCannotExpandAuthority(f *testing.F) {
    ...
}
```

This is excellent interview material.

---

# 33. Tamper Testing

Create envelope.

Modify:

```json
"max_refund": 500
```

to:

```json
"max_refund": 5000
```

without resigning.

Verification must fail.

```text
SIGNATURE_INVALID
```

---

# 34. Success Metrics

Technical MVP success:

```text
≥ 200 adversarial test cases

100% detection of known authority-expansion cases

100% detection of intentionally modified signed envelopes

p95 policy evaluation < 10 ms

p95 gateway overhead < 25 ms

≥ 10,000 automated handoff simulations

≥ 80% core-package test coverage
```

External validation:

```text
3 external developers run it

1 external contributor

100 GitHub stars = nice
250+ GitHub stars = strong
500+ GitHub stars = excellent

At least 1 external team integrates the gateway
```

Do not fabricate these numbers on your résumé. Add them only after achieving them.

---

# 35. Six-Week Development Plan

## Week 1 — Core specification

Build:

* envelope Go structs
* JSON schema
* canonical serialization
* Ed25519 signing
* signature verification
* expiration validation
* parent relationship

Deliver:

```text
handoffguard envelope create
handoffguard envelope verify
```

---

## Week 2 — Policy Engine

Build:

* action subset comparison
* deny policy inheritance
* resource narrowing
* expiration rules
* delegation depth
* approval inheritance
* CEL expressions

Deliver:

```text
handoffguard policy diff
```

---

## Week 3 — Server + PostgreSQL

Build:

* HTTP API
* PostgreSQL schema
* envelope persistence
* handoff events
* approvals
* violations
* audit events

---

## Week 4 — MCP Gateway

Build:

```text
Agent → Gateway → MCP server
```

Support:

* tool discovery
* tool forwarding
* action authorization
* audit logging

Build the vulnerable refund demo.

---

## Week 5 — Agent Integration

Integrate one real multi-agent framework.

Demonstrate:

```text
Support
→ Billing
→ Notification
```

Build:

* handoff adapter
* propagation
* enforcement
* tracing

---

## Week 6 — Production Polish

Build:

* GitHub Action
* Docker Compose
* benchmark suite
* CLI improvements
* documentation
* minimal web dashboard
* architecture diagrams
* recorded demo

---

# 36. Optional Weeks 7–8

Only after the core product works:

* Redis revocation cache
* Slack approvals
* Google ADK integration
* policy registry
* hosted demo
* load tests
* authentication
* organization support

Do not build these before the core invariant works.

---

# 37. Launch Strategy

Launch HandoffGuard as open source.

Primary audience:

```text
AI engineers
platform engineers
security engineers
MCP developers
agent framework developers
```

Publish:

### GitHub

Excellent README with:

```text
The Problem
5-minute Demo
Architecture
Threat Model
Benchmarks
```

---

### Technical article

Title:

**Your AI Agent Has Permissions. What Happens When It Delegates Them?**

Explain the security problem rather than promoting the project.

---

### Hacker News

Launch as:

```text
Show HN: HandoffGuard – prevent AI agents from delegating away security constraints
```

---

### Reddit

Relevant communities:

```text
r/golang
r/LocalLLaMA
r/MachineLearning
r/cybersecurity
AI agent communities
```

---

# 38. Validation Before Building Everything

Before spending eight weeks, validate the core problem.

Build only this first:

```text
Parent Envelope
     ↓
Child Envelope
     ↓
Policy Diff
     ↓
ALLOW / DENY
```

Create a working CLI.

Then contact engineers building agents.

Ask:

> "When one of your agents delegates work to another, how do you ensure permissions, approvals and data-use restrictions survive the handoff?"

Do NOT ask:

> "Would you use HandoffGuard?"

The first question discovers whether the pain exists.

Target:

```text
10–15 conversations
```

If multiple engineers say:

```text
"We built this ourselves."

"We don't have a good solution."

"We rely on prompts."

"We're worried about this."
```

continue building.

---

# 39. Competitive Positioning

Do not claim:

> "Nobody has ever built anything like this."

That is nearly impossible to prove.

Say:

> HandoffGuard focuses specifically on deterministic constraint inheritance and authority narrowing across heterogeneous agent-to-agent delegation.

Adjacent markets already contain:

```text
agent observability
agent guardrails
MCP security gateways
agent identity products
AI governance
tool authorization
```

HandoffGuard should deliberately avoid becoming another generic product in those categories.

Its wedge is:

```text
SECURITY INHERITANCE
DURING DELEGATION
```

---

# 40. Technical Differentiator

The project must not depend on:

```text
GPT judges
Claude judges
LLM policy classification
```

Core enforcement is classical computer science:

```text
authorization
partial ordering
set relationships
cryptography
distributed tracing
policy evaluation
graph traversal
revocation
immutable evidence
```

AI is the workload being secured.

AI is not the security mechanism.

That distinction makes the project considerably stronger.

---

# 41. Interview Story

When a recruiter asks:

> "Tell me about HandoffGuard."

Answer:

> HandoffGuard is an authorization inheritance layer I built for multi-agent AI systems. I noticed that agent frameworks provide tool permissions and guardrails, but when one agent delegates work to another, there's no universal guarantee that the original security constraints survive. I designed signed obligation envelopes and a deterministic policy engine that allows agents to narrow delegated authority but prevents them from expanding permissions or removing mandatory approvals.

Then explain:

```text
Ed25519
Go
PostgreSQL
MCP proxy
CEL
OpenTelemetry
property-based testing
policy diffing
CI/CD
```

That turns the discussion into a software-engineering conversation rather than an "AI wrapper" conversation.

---

# 42. Future Roadmap

## V1

Constraint inheritance.

---

## V2

Cross-framework agent identity.

```text
OpenAI Agent
     ↓
LangGraph Agent
     ↓
Google ADK Agent
```

Same envelope.

---

## V3

Distributed revocation.

Administrator can revoke:

```text
root envelope
```

and terminate every descendant delegation.

---

## V4

Policy Registry

Organizations maintain:

```text
finance-agent-policy
customer-support-policy
production-infra-policy
health-data-policy
```

---

## V5

Delegation Risk Analysis

Analyze graphs such as:

```text
A
├── B
│   ├── C
│   └── D
└── E
    └── F
```

Detect:

```text
unexpected depth
high privilege concentration
cross-boundary data access
long-lived authority
```

---

## V6

Enterprise Hosted Platform

Potential commercial features:

* SSO
* RBAC
* policy registry
* SIEM integrations
* managed signing keys
* compliance reporting
* agent inventory
* centralized revocation
* organization-wide delegation graphs

---

# 43. MVP Definition of Done

The project is ready for launch when a fresh developer can run:

```bash
git clone ...

docker compose up
```

and execute:

```bash
make demo
```

The demo must show:

```text
1. Agent A receives constrained authority.

2. Agent A delegates to Agent B.

3. Valid delegation succeeds.

4. Agent B attempts to remove a mandatory requirement.

5. HandoffGuard blocks the delegation.

6. Agent B attempts an unauthorized tool call.

7. HandoffGuard blocks the action.

8. Manager approval is created.

9. Authorized action succeeds.

10. Audit chain verifies successfully.
```

The complete story should take under three minutes.

---

# 44. Résumé Positioning

Once completed, the project should be presented as infrastructure/security engineering rather than simply an AI project.

Example:

**HandoffGuard | Go, PostgreSQL, MCP, OpenTelemetry, CEL, Docker**

* Built an open-source authorization layer for multi-agent AI systems that cryptographically propagates security constraints across agent-to-agent delegation using signed Ed25519 obligation envelopes.
* Designed a deterministic policy engine preventing privilege expansion, resource-scope widening, dropped approvals, and delegation-depth violations across autonomous workflows.
* Implemented an MCP enforcement proxy, tamper-evident audit chain, policy-diff CI gate, and fuzz-testing suite covering 200+ adversarial delegation scenarios with sub-25 ms target enforcement overhead.

Replace target numbers with actual benchmark results before putting them on the résumé.

---

# 45. Product Principle

The design principle that should guide the entire project is:

> **Agents may delegate work. They must not be able to delegate away the rules.**

