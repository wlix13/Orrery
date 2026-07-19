<!--
PR TITLE: write a short, meaningful summary of the change as a whole, in the
imperative mood - don't leave the auto-filled first-commit message. Examples:
  * Add online users to the node API
  * Improve CI/CD caching and job layout
  * Fix double-counted traffic after an Xray restart
Individual commits should still follow Conventional Commits (see CONTRIBUTING.md);
on merge/rebase those commits drive the release version and changelog.
-->

## 🏷️ Type of change

<!-- Please check all that apply - this drives the PR's type labels. -->

- [ ] **Bug fix** (fixes an issue in collection, storage, API or dashboard behaviour)
- [ ] **Feature** (adds a new metric, endpoint, backend or dashboard view)
- [ ] **Enhancement** (improves existing collection, queries or UI)
- [ ] **Refactor** (restructures code without changing behaviour)
- [ ] **Breaking change** (changes `orrery.yaml`, the API contract or the storage schema)
- [ ] **Security** (auth, scoping, host-key verification or other hardening)

## 📝 Description

### Why is this change needed?

<!-- Explain the motivation and context for this change -->

### 🔗 Related Issues

<!-- Link to related issues using "Fixes #123", "Closes #123", or "Relates to #123" -->

## 🧪 Testing

<!-- Describe how you tested your changes -->

- [ ] `task ci` passes locally
- [ ] Checked against a real node (`orrery probe <fleet>/<id>`)
- [ ] No testing required (documentation changes only)

## ✅ Checklist

<!-- Ensure all applicable items are completed before requesting review -->

- [ ] Code follows project style guidelines
- [ ] Self-review completed
- [ ] Linter and typechecks pass (`task lint` and `task typecheck`)
- [ ] Both build variants still build (`task all` and `task build:nodashboard`)
- [ ] Documentation updated (if applicable)
