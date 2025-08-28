# Commit Message Guidelines

This document outlines the commit message format and conventions for the Antimetal Agent project.
We have some precise rules on how our commit messages are formatted.
This format ultimately leads to easier to read git history and makes it easier to autogenerate our release changelog.

## Commit Message Format

Each commit message consists of a **header**, a **body**, and a **footer**:

```
<type>(<scope>): <subject>
<BLANK LINE>
<body>
<BLANK LINE>
<footer>
```

The **header** is mandatory and the **scope** of the header is optional.

Any line of the commit message cannot be longer than 100 characters!
This allows the message to be easier to read on GitHub as well as in various git tools.

### Type

Must be one of the following:

- **feat**: A new feature
- **fix**: A bug fix
- **docs**: Documentation only changes
- **refactor**: A code change or styling changes that neither fixes a bug nor adds a feature
- **perf**: A code change that improves performance
- **test**: Adding new tests, missing tests or correcting existing tests
- **ci**: Changes to our CI infrastructure
- **build**: Changes to our build infrastructure
- **chore**: Changes to auxiliary tools and libraries and other grunt tasks 
- **deps**: Changes to dependencies
- **license**: Updates to source code licenses

### Scope

The scope should be the name of the component affected (as perceived by the person reading the changelog).

The following is the list of supported scopes:

#### **Control Plane Client**
- **`api`** - Protocol buffer definitions, gRPC service definitions
- **`config`** - Config agent

#### **Infrastructure Graph**
- **`intake`** - gRPC intake worker, streaming, batching
- **`resource`** - Resource store

#### **Kubernetes Agent**
- **`k8s`** - Kubernetes controller, agent, indexer, handler changes

#### **Performance Monitoring Agent**
- **`perf`** - Performance monitoring system and collectors
- **`ebpf`** - eBPF programs

### Subject

The subject contains a succinct description of the change:

- Use the imperative, present tense: "change" not "changed" nor "changes"
- Don't capitalize the first letter
- No dot (.) at the end

### Body

Just as in the **subject**, use the imperative, present tense: "change" not "changed" nor "changes".
The body should include the motivation for the change and contrast this with previous behavior.

### Footer

The footer should contain any information about **Breaking Changes** and is also the place to reference GitHub issues that this commit **closes**.

#### Commit Signoffs

**All commits must be signed-off** using `git commit --signoff` (or `-s` for short).
This adds a `Signed-off-by` line to the commit message, indicating that you certify the commit according to the Developer Certificate of Origin.

#### LLM Contributions

If Claude Code or any other LLM contributed to the commit, then they **must** be included as a coauthor.

## Examples

```
feat(perf): add CPU frequency monitoring

Add support for monitoring CPU frequency scaling by reading
/sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq files.

Closes #123

Co-Authored-By: Claude <noreply@anthropic.com>
Signed-off-by: John Doe <john.doe@example.com>
```

```
feat(ebpf): implement CO-RE support for execsnoop

Add compile-once-run-everywhere support for execsnoop eBPF program
to improve portability across different kernel versions.

Signed-off-by: John Doe <john.doe@example.com>
```

```
fix(resource): prevent potential deadlock in BadgerDB transactions

Use read-only transactions for query operations to avoid blocking
writers during concurrent access patterns.

Fixes #456

Signed-off-by: John Doe <john.doe@example.com>
```

```
chore(deps): update controller-runtime to v0.19.0

Signed-off-by: John Doe <john.doe@example.com>
```

```
chore(license): update license

Signed-off-by: John Doe <john.doe@example.com>
```

```
docs(perf): add performance collector development guide

Document standardized patterns for implementing new performance
collectors including constructor patterns, error handling, and
testing methodology.

Signed-off-by: John Doe <john.doe@example.com>
```

## Scope Selection Guidelines

### **Omit Scope When:**
- Changes are very broad and affect multiple unrelated areas
- The change is a major refactor touching many components
- Simple one-word changes that don't benefit from categorization

### Multi-Component Changes

For commits affecting multiple components:

1. **Use the most significant component** as the scope if one component is primary
2. **Use a broader scope** (e.g., `perf` for multiple collectors, `k8s` for multiple Kubernetes components)  
3. **Use no scope** for very broad architectural changes

## Reverting changes

If the commit reverts a previous commit, it should begin with `revert:`, followed by the header of the reverted commit.
In the body it should say: `This reverts commit <hash>.`, where the hash is the SHA of the commit being reverted.

