---
name: backlog-loop
description: Pick up one task from a backlog project and iterate on it until a Judge sub-agent verifies it is genuinely done, then exit. The "loop" is internal to a single task - implement, verify, judge, fix, re-judge - not across multiple tasks. Invoke as `/backlog-loop <project>` to pull the next highest-priority `todo` task, or `/backlog-loop <project> <ref>` to target a specific task. Use when the user wants the agent to clear one item off the backlog with a built-in verification gate.
---

# backlog-loop

One task in. Iterate until a skeptical Judge says it's actually done. Exit.

The name is a callback to the Ralph loop: the loop is on a single task - implement → verify → judge → fix → re-judge - bounded by a max iteration count. This skill never picks up a second task.

## Requires: the `backlog` skill

Load `skills/backlog/skill.md` first. It is the canonical reference for every CLI flag, command, ID format (`TASK-N` / bare integer / ULID), JSON shape, and enum value. This skill assumes you know how to drive the CLI.

Passera alltid `--profile default` och `--as "ai:<modell>"` (den körande modellens exakta ID) på varje skrivning.

## Invocation

```
/backlog-loop <project>          # pull next todo task from <project>, iterate to done, exit
/backlog-loop <project> <ref>    # work a specific task (TASK-N, bare integer, or ULID)
/backlog-loop help               # print the headless-execution help and exit
```

If the user omits `<project>` (and didn't pass `help`), list available projects and ask which one - do not guess:

```sh
backlog --profile default project list --json
```

If the user passes `help`, point to the headless doc (see **Headless execution** below) and exit. Do not pick up a task.

## Hard rules

- **One task only.** The skill never moves to a second task. The user invokes it again if they want more.
- **Mandatory Judge gate.** The task only moves to `done` after a `[JUDGE RECEIPT]` says PASS. The implementing agent never self-marks `done`.
- **Max 5 attempts.** Implement → verify → judge counts as one attempt. After 5 failed Judge verdicts, stop and mark the task blocked with a full diagnosis.
- **Never expand scope silently.** Work discovered mid-execution that's outside the task → new task, not silent expansion of this one.
- **Always attribute writes.** `--as "ai:<modell>"` on every write.
- **Always `--profile default`**.
- **The Judge never calls advisor.** The Judge sub-agent already *is* the critical-review step - advisor-ing the advisor just adds latency for no independent signal. See the Judge prompt template's hard rules.

## Workflow

### 1. Select the task

If a ref was provided:

```sh
backlog --profile default task show <ref> --json
```

Otherwise:

```sh
backlog --profile default task list --project <project> --status todo --sort priority --json
```

Pick the **first** task. Sort `priority` orders P1 → P5; within priority, lower `seq` first. If the result is empty, tell the user "no todo tasks in `<project>`", show what's `doing`, and exit. Do not invent work.

Capture from the JSON: `id` (ULID), `seq` (`TASK-N`), `title`, `description`, `type`, `priority`, `labels`, any attached `plans`.

### 2. Check the description is judgeable

The Judge needs verifiable acceptance criteria to do its job. Read the description fully and decide whether it contains them.

**Sufficient:** explicit acceptance criteria (a checklist, or sentences naming the observable outcome) AND at least one verification approach (a command, a check, or "manual: <what to inspect>"). The structured shape produced by `backlog-enhance-tasks` (Context / Acceptance criteria / Implementation hints / Verification) is the ideal.

**Insufficient:** empty description, one-line title-only, or vague language with no observable signal of done.

If insufficient, **stop**. Post a comment and exit without picking up:

```sh
backlog --profile default comment add \
  "Kan inte plockas upp via /backlog-loop: beskrivningen saknar judgeable acceptanskriterier. Saknas: <kriterier | verifieringskommandon | båda>. Kör /backlog-enhance-tasks TASK-N först." \
  --task TASK-N --as "ai:<modell>"
```

This is non-negotiable. A loop without a Judge gate is the failure mode this skill exists to prevent; a Judge without criteria is theater.

### 3. Move to doing

```sh
backlog --profile default task move TASK-N --status doing --as "ai:<modell>"
backlog --profile default comment add \
  "Upplockad via /backlog-loop. Startar försök 1 av max 5." \
  --task TASK-N --as "ai:<modell>"
```

### 4. Attach a plan if non-trivial

A task is non-trivial if any of: touches more than one file, mixes concerns, has more than two acceptance criteria, or requires reasoning that a future reader would want to see.

```sh
backlog --profile default plan add \
  --task TASK-N --title "Implementation plan" \
  --content "$(cat <<'EOF'
## Steps
1. <first concrete action - file, function, what to change>
2. <next action>

## Testing
- <how to verify against each acceptance criterion>

## Risks
- <risk + mitigation>
EOF
)" --as "ai:<modell>" --json
```

Skip the plan for trivial single-file edits.

### 5. The iterate-until-judged loop

Run up to **5 attempts**. Each attempt is one full pass through implement → verify → judge.

```
for attempt in 1..5:
  implement (or refine based on the prior Judge receipt)
  run verification commands
  dispatch Judge sub-agent
  if Judge says PASS: break, go to step 6
  if Judge says FAIL: read the failure, plan the fix, continue
after 5 failures: go to step 7 (blocked)
```

**5a. Implement (attempt N)**

If `N == 1`: implement from scratch against the description and any attached plan.

If `N > 1`: read the prior `[JUDGE RECEIPT]` from the task's comments. The Judge's `criteria` list (each marked PASS or FAIL) and `verification_commands` output tell you exactly what to fix. Don't rebuild what already passes. Don't argue with the Judge - fix the failing criterion.

For larger work, delegate to a sub-agent via the `Agent` tool (`general-purpose` for implementation, `Explore` for research). Pass the task description, the failing criteria from the last Judge receipt (if any), and the verification command. The sub-agent must not move the board.

Drop progress comments at meaningful milestones:

```sh
backlog --profile default comment add \
  "Försök N: ändrade <fil:rad>, la till test i <fil:rad>." \
  --task TASK-N --as "ai:<modell>"
```

**5b. Run verification**

Run every verification command listed in the description. Capture stdout/stderr and exit codes. If a command fails, attempt a fix (up to 2 tries) before handing the result to the Judge. If verification is "manual: <inspect X>", note that and let the Judge handle it.

**5c. Dispatch the Judge**

Use the `Agent` tool with `subagent_type: "general-purpose"` and a fully self-contained prompt. The Judge has no memory of this session.

Judge prompt template (paste verbatim with substitutions):

```
You are the Judge for backlog task TASK-N in project <project>. This is attempt N of 5.

Read the task description in full:
  backlog --profile default task show TASK-N --json | jq -r '.description'

Your job:

1. For EACH acceptance criterion in the description, determine PASS or FAIL by inspecting the actual repo state. Read files. Cite file:line evidence for each verdict.
2. Run EVERY verification command listed in the description. Capture stdout/stderr and exit code. Non-zero exit is a FAIL unless the description explicitly says otherwise.
3. Look for proxy signals being mistaken for completion - flag any of these and FAIL the corresponding criterion:
   - "Tests pass" without the test actually exercising the criterion
   - "Files changed" without behavior changed
   - "Plan written" instead of implemented
   - "Build succeeded" without the output behaving correctly
   - "Lint clean" - orthogonal to the requirement
   - The implementing agent's own claim of done
4. Look for adjacent breakage - things outside the criteria that a reasonable reviewer would flag (a broken test elsewhere, an obvious bug introduced). Note these as `judge_observations`. They do not fail the task on their own but must be reported.
5. Return a verdict: PASS or FAIL.

Hard rules:
- You are READ-ONLY. Do not edit files. Run only verification commands and read commands.
- Be skeptical by default. A criterion is FAIL until evidence convincingly shows PASS.
- If a criterion is unverifiable as written, FAIL it and explain what the criterion would need to say to be verifiable.
- Do not move the task on the board. Return only the receipt.
- Do NOT call the advisor tool. You are already the critical-review step for this task - calling advisor from inside the Judge is a redundant second opinion on top of the review you were dispatched to perform, and it adds significant latency for no gain. Do your own skeptical, evidence-based review directly.

Output format (exact, parseable):

[JUDGE RECEIPT]
result: done
attempt: <N>
verdict: PASS | FAIL
criteria:
  - [PASS|FAIL] <criterion text> - <evidence: file:line, command exit code, or quoted output>
  - ...
verification_commands:
  - `<command>` → exit <code>
    <relevant output snippet>
  - ...
judge_observations:
  - <optional: adjacent issues outside the criteria>
reason: <one paragraph rationale for the verdict>
next_fix_hint: <if FAIL: one sentence telling the implementing agent what to focus on next>
```

**5d. Post the Judge receipt**

Post the full Judge output as a comment, prefixed exactly so the next iteration can parse it:

```sh
backlog --profile default comment add "$(cat <<'EOF'
<paste the Judge's full output verbatim, including the [JUDGE RECEIPT] header>
EOF
)" --task TASK-N --as "ai:<modell>"
```

**5e. Branch on the verdict**

- **PASS:** break out of the loop. Go to step 6.
- **FAIL:** record the iteration and continue the loop with attempt N+1. Read `next_fix_hint` and the failing criteria - those drive the next attempt's implementation.

### 6. Complete

When the Judge returns PASS:

```sh
backlog --profile default comment add "$(cat <<'EOF'
Klar via /backlog-loop. Judge godkände på försök <N>.

Ändrat:
- <fil:rad-sammanfattning>

Verifierat:
- `<verifieringskommando>` → exit 0

Följduppgifter:
- TASK-M (<titel>) - om några
EOF
)" --task TASK-N --as "ai:<modell>"

backlog --profile default task move TASK-N --status done --as "ai:<modell>"
```

Print a one-line summary to the user and **stop**:

```
Worked TASK-N (<title>) → done. Judge passed on attempt <N>. Verified: <command> exit 0.
```

### 7. Blocked (max attempts reached or hard blocker)

If the Judge has rejected 5 consecutive attempts, or you hit a real blocker (needs credentials, a product decision, a destructive op, a criterion that is unverifiable as written and the user must clarify), do **not** mark `done`. Move back to `todo` with a full diagnosis:

```sh
backlog --profile default comment add "$(cat <<'EOF'
Blockerad efter <N> försök via /backlog-loop.

Varför blockerad:
<ett stycke - vad som fortsatte faila, eller vad som behövs utifrån>

Failande kriterier efter senaste Judge-passet:
- <kriterium> - <belägg Judge angav>

Vad som provats:
- Försök 1: <en rad>
- Försök 2: <en rad>

Vad som behövs för att komma loss:
- <konkret åtgärd av Rasmus eller annan task>

Föreslaget nästa steg:
- Skärp acceptanskriterierna (/backlog-enhance-tasks TASK-N)
- ELLER dela i en mindre task med snävare scope
- ELLER tillhandahåll <credential | beslut | referens> och kör /backlog-loop igen
EOF
)" --task TASK-N --as "ai:<modell>"

backlog --profile default task move TASK-N --status todo --as "ai:<modell>"
```

Print:

```
TASK-N (<title>) → blocked after <N> attempts. See comment for diagnosis.
```

Stop.

### 8. Discovery during execution

If during any attempt you find related work outside the task's scope - a missing test elsewhere, a refactor that would help, a broken adjacent function - **create a new task** rather than expanding this one:

```sh
backlog --profile default task add --project <project> \
  --title "<imperative + specific>" \
  --description "Avknoppad från TASK-N. <kontext>" \
  --type <task|bug|chore> --priority <P1-P5> \
  --as "ai:<modell>" --json
```

Comment on TASK-N linking the new TASK-M. The Judge does not consider spawned tasks part of TASK-N's completion criteria.

## Why a Judge for a single task

Without the Judge, this skill is just "do one task and exit" - the agent self-marks `done` and the same laziness failure mode from the Codex Goal lessons applies (tests pass ≠ feature works; files changed ≠ behavior changed). The Judge enforces that completion is observable, not asserted.

The Judge is light: one task, one set of criteria, no cross-checkpoint state. The contract: read-only, evidence-based, skeptical default, reject proxy signals, never picks the next active task.

## Headless execution

Referensen för headless-, cron- och CI-körning (`claude -p`, drain-skript,
GitHub Actions, miljövariabler, observability) ligger som backlog doc i
projektet infra: kör `backlog doc list --project infra --profile default`
och öppna dokumentet "backlog-loop headless-drift". Vid `/backlog-loop help`:
hänvisa dit och avsluta.

Obs: kommentarsmallarna ovan är försvenskade - blockerade tasks känns igen
på "Blockerad efter", inte "Blocked after".

## Quick command reference

```sh
backlog --profile default project list --json
backlog --profile default task list --project <project> --status todo --sort priority --json
backlog --profile default task show TASK-N --json
backlog --profile default task move TASK-N --status doing --as "ai:<modell>"
backlog --profile default task move TASK-N --status done  --as "ai:<modell>"
backlog --profile default task move TASK-N --status todo  --as "ai:<modell>"
backlog --profile default plan add --task TASK-N --title "..." --content "..." --as "ai:<modell>" --json
backlog --profile default comment add "..." --task TASK-N --as "ai:<modell>"
backlog --profile default task add --project <project> --title "..." --description "..." --as "ai:<modell>" --json
```

See `skills/backlog/skill.md` for the full CLI surface.
