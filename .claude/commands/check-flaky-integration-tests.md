---
description: Check integration tests for common flaky test patterns
---

Analyze integration test files in the tests/ directory for flaky test patterns based on fixes from PR #1742.

## Three Flaky Test Patterns

1. **Bare `Expect()` in Eventually/Consistently** → Use `g.Expect()`
2. **Get-Update NOT in Eventually blocks** → Wrap in Eventually
3. **Missing assertions** → Add `.To()` or `.Should()`

---

## Pattern 1: Bare Expect() in Eventually/Consistently

**Rule:** Use `g.Expect()` instead of bare `Expect()` inside Eventually/Consistently blocks.

**Why it's flaky:** Bare `Expect()` panics on failure instead of allowing retry.

**Detection:**
- Search: `Eventually\(func\(` and `Consistently\(func\(`
- Read next 30-50 lines (or until closing `})`) to capture entire block
- Look for bare `Expect(` (not `g.Expect`) anywhere in the block
- Report violations

**Example:**
```go
// ❌ BAD
Eventually(func(g Gomega) {
    err := k8sClient.Get(ctx, key, obj)
    Expect(err).To(Succeed())  // Should be g.Expect!
})

// ✅ GOOD
Eventually(func(g Gomega) {
    err := k8sClient.Get(ctx, key, obj)
    g.Expect(err).To(Succeed())
})
```

---

## Pattern 2: Get-Update NOT in Eventually Blocks

**Rule:** Get-Modify-Update sequences MUST be wrapped in `Eventually(func(g Gomega)` to handle transient conflicts.

**Why it's flaky:** Resource version conflicts cause test failures without retry.

**Detection:**
1. Search: `\.Update\(ctx,` (exclude `Status().Update`)
2. For each, read preceding 50-100 lines to find potential Get call
3. Check if:
   - `.Get` exists in those lines (indicator of Get-Update pattern)
   - `Eventually(func(g Gomega)` wraps BOTH Get and Update
4. **Critical violation:** Get-Update found but NOT wrapped in Eventually(func(g Gomega)
   - Note: `Eventually(testClient().Get)` does NOT count - Update must be inside Eventually too
5. **Note:** For complex test cases, read more context if Get call not found in 100 lines

**Example:**
```go
// ❌ BAD - Not in Eventually
err := testClient().Get(ctx, key, kuadrantObj)
Expect(err).To(Succeed())
kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: true}
err = testClient().Update(ctx, kuadrantObj)
Expect(err).To(Succeed())

// ❌ BAD - Update outside Eventually
Eventually(testClient().Get).WithArguments(key, kuadrantObj).Should(Succeed())
kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: true}
Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())  // Outside!

// ✅ GOOD - Both wrapped in Eventually
Eventually(func(g Gomega) {
    g.Expect(testClient().Get(ctx, key, kuadrantObj)).To(Succeed())
    kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: true}
    g.Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())
}).WithContext(ctx).Should(Succeed())
```

---

## Pattern 3: Missing Assertions

**Rule:** All `Expect()` and `Eventually()` calls must have assertion matchers (`.To()`, `.Should()`, etc.).

**Why it's flaky:** Without assertions, conditions aren't actually checked, leading to silent failures.

**Detection:**
1. Search for `Expect\(` calls - read enough lines to find the closing `)` and check if followed by `.To`, `.Should`, `.NotTo`, `.ToNot`
2. Search for `Eventually\(` calls - read enough lines to find the closing `)` and check if followed by `.Should`, `.WithContext(...).Should`, or `.To`
3. Look for function calls that return bool/error but aren't wrapped in assertions
4. If closing found without assertion matcher, it's a violation
5. Use sufficient context (20-50 lines) to handle multi-line statements

**Example:**
```go
// ❌ BAD - Missing .To(BeTrue())
Expect(tests.RLPEnforcedCondition(ctx, testClient(), key, reason, msg))

// ❌ BAD - Eventually without .Should()
Eventually(func() bool {
    return wasmPlugin.Status.Ready
})

// ✅ GOOD - Has assertion
Expect(tests.RLPEnforcedCondition(ctx, testClient(), key, reason, msg)).To(BeTrue())

// ✅ GOOD - Wrapped with assertion
Eventually(func() bool {
    return tests.RLPEnforcedCondition(ctx, testClient(), key, reason, msg)
}).WithContext(ctx).Should(BeTrue())
```

---

## Execution Steps

### Step 1: Pattern 1 - Bare Expect
1. Grep: `Eventually\(func\(` and `Consistently\(func\(`
2. Read next 30-50 lines after each match (or until closing `})`)
3. Find bare `Expect(` anywhere in the block (not `g.Expect`)
4. Report: file, line, code snippet

### Step 2: Pattern 2 - Get-Update Not in Eventually
1. Grep: `\.Update\(ctx,` (exclude `Status().Update`)
2. Read 50-100 lines before each Update (enough to find Get call)
3. Check for:
   - `.Get` present? (if no, skip - not a Get-Update pattern)
   - `Eventually(func(g Gomega)` wrapping both Get and Update? (if no, CRITICAL)
4. If uncertain, read more context to locate the Get call
5. Report: file, line, code snippet

### Step 3: Pattern 3 - Missing Assertions
1. Grep: `Expect\(` - for each match:
   - Read next 20-30 lines (enough to capture multi-line statements)
   - Look for closing `)` followed by `.To(`, `.Should(`, `.NotTo(`, `.ToNot(`
   - If closing `)` found without assertion matcher, it's a violation
2. Grep: `Eventually\(` - for each match:
   - Read next 30-50 lines (to capture entire block)
   - Look for pattern: `)` followed by `.Should(`, `.WithContext(`, or `.To(`
   - If closing `)` found without assertion, it's a violation
3. **Note:** Some long blocks may exceed line limits - use judgment and read more context if needed
4. Report: file, line, code snippet

### Step 4: Report

**Summary:**
- Total violations by pattern and severity
- Files affected

**Details for each violation:**
```
Pattern X: [SEVERITY] Description
File: path/to/file.go:LineNumber

[5-10 lines of code showing the issue]

❌ Problem: [brief explanation]
✅ Fix: [brief fix recommendation]
```

**Severity levels:**
- **CRITICAL:** Get-Update not in Eventually
- **HIGH:** Bare Expect in Eventually (has g param), Missing assertions
- **MEDIUM:** Eventually without g Gomega parameter

---

## Quick Reference

| Violation | Severity | Fix |
|-----------|----------|-----|
| Get-Update outside Eventually | CRITICAL | Wrap in `Eventually(func(g Gomega)` |
| Bare `Expect` inside Eventually | HIGH | Change to `g.Expect` |
| Missing `.To()` or `.Should()` | HIGH | Add assertion matcher |
| Eventually without `g Gomega` | MEDIUM | Add `g Gomega` parameter |

---

## Real Examples from PR #1742

**Fix 1: Wrap Get-Update in Eventually**
```diff
- err := testClient().Get(ctx, key, kuadrantCR)
- Expect(err).NotTo(HaveOccurred())
- kuadrantCR.Spec.Observability.Enable = true
- err = testClient().Update(ctx, kuadrantCR)
+ Eventually(func(g Gomega) {
+     g.Expect(testClient().Get(ctx, key, kuadrantCR)).To(Succeed())
+     kuadrantCR.Spec.Observability.Enable = true
+     g.Expect(testClient().Update(ctx, kuadrantCR)).To(Succeed())
+ }).WithContext(ctx).Should(Succeed())
```

**Fix 2: Add Missing Assertion**
```diff
- Expect(tests.RLPEnforcedCondition(ctx, testClient(), key, reason, msg))
+ Expect(tests.RLPEnforcedCondition(ctx, testClient(), key, reason, msg)).To(BeTrue())
```

**Fix 3: Wrap in Eventually**
```diff
- Expect(tests.RLPEnforcedCondition(...)).To(BeTrue())
+ Eventually(func() bool {
+     return tests.RLPEnforcedCondition(...)
+ }).WithContext(ctx).Should(BeTrue())
```
