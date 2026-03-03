# Code agent skills policy

## 1. Purpose

This repository standardizes how the team installs and manages **agent skills** using the [Skills CLI](https://github.com/vercel-labs/skills) (`npx skills`).

The goals are:

* **Consistency:** Everyone uses the same set of skills across the team.
* **Reproducibility:** A new environment can restore the team-approved skills reliably.
* **Low-friction onboarding:** New members can install skills with a single command.

---

## 2. Source of Truth

### 2.1 Lockfile as the Team Contract

The repository maintains **`skills-lock.json`** as the single source of truth for **which skills the team uses**.

* `skills-lock.json` **must be committed** to the repository.
* Changes to skills must be reflected by updating this lockfile.

The lockfile enables restoring the same skills set on another machine through the CLI restore/install workflow.

---

## 3. Installation Directory (Project Scope)

### 3.1 Canonical Installation Path

As of today, Skills CLI installs project-scoped skills into:

* `/.agents/skills`

This directory is considered the **canonical local installation output** for this repo.

> Note: Some code agents do not read `.agents/skills` by default (e.g., Claude Code may expect a different directory). See section 6.

---

## 4. Onboarding: New Developer Setup

After cloning the repository, a new team member should run the following command from the repo root:

```bash
npx skills experimental_install
```

This restores the team’s skills based on `skills-lock.json`.

---

## 5. Team Workflow

### 5.1 Adding / Updating / Removing Skills

Any operation that changes the set of team skills must:

1. Be performed using **Skills CLI**
2. Update the project’s **`skills-lock.json`**
3. Include the lockfile update in the same PR/commit

Recommended review rule:

* **PRs that change skills MUST include `skills-lock.json` changes.**

### 5.2 Code Review Expectations

Reviewers should verify:

* `skills-lock.json` is updated appropriately
* No undocumented manual edits were made to the installed output directories
* Onboarding remains a one-command flow (`experimental_install`)

---

## 6. Agents That Do Not Use `.agents/skills`

### 6.1 Current Limitation

At the time of writing, Skills CLI installs into **`.agents/skills` only**.

If you use a code agent that **does not discover skills from `.agents/skills`** (e.g., **Claude Code** or other agents with different conventions), you must **sync** the installed skills directory manually.

### 6.2 Sync Options

#### Option A — Symlink (Recommended)

Symlinking keeps `.agents/skills` as the single canonical install output and avoids duplication.

#### Option B — Manual Copy

If symlinks are not supported in your environment, copy the directory:

```bash
rm -rf .claude/skills
mkdir -p .claude
cp -R .agents/skills .claude/skills
```

> If you choose copy, you must repeat the copy step whenever the skills set changes.

---

## 7. Repository Hygiene

### 7.1 What Must Be Committed

* `skills-lock.json` (required)

### 7.2 What Should NOT Be Manually Edited

* `/.agents/skills/**` should be treated as **CLI-managed output**
* agent-specific skill folders such as `/.claude/skills/**` (if used) should be treated as **derived outputs**

If your team wants to commit installed skill output directories (rarely recommended), define that explicitly in a separate policy section. Otherwise, treat them as generated artifacts.

---

## 8. Policy Enforcement (Recommended)

To keep the repo consistent, consider adding CI checks that enforce:

* `skills-lock.json` exists and is tracked
* Changes to skills must be accompanied by lockfile updates
* Derived agent folders (if any) are not modified manually (or are ignored)

---

## 9. Summary

* The team manages skills using **Skills CLI** (`npx skills`)
* The project commits **`skills-lock.json`** as the shared lockfile
* New developers run **`npx skills experimental_install`** to restore skills
* Skills are installed into **`.agents/skills`**
* If your code agent requires a different directory (e.g., Claude Code), you must **symlink or copy** from `.agents/skills`
