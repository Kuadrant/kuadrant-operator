---
description: Check integration tests for common flaky test patterns
---

Analyze all *_test.go files in the tests directory for common flaky test patterns that cause intermittent CI/CD failures based on the patterns identified in recent fixes.

## Summary: Three Critical Flaky Test Patterns

1. **Pattern 1:** Bare `Expect()` in Eventually/Consistently → Use `g.Expect()`
2. **Pattern 2:** Get-Modify-Update → Use Patch (SSA or MergeFrom)
3. **Pattern 3:** Status().Update() → Use Status().Patch() with MergeFrom

## When to Use Which Patch Strategy:

| Operation | Strategy | Eventually Needed? | Key Advantage |
|-----------|----------|-------------------|---------------|
| Status updates | Status().Patch() with MergeFrom | Yes | Prevents losing concurrent status changes |
| Removing fields (= nil) | MergeFrom Patch | Yes | Only MergeFrom can remove fields |
| Kuadrant CR add/update | Server-Side Apply (SSA) | No | Simpler - no Get, no Eventually needed |
| Complex spec updates | MergeFrom Patch | Yes | Works for all cases, safer |

## Pattern 1: Expect() without Gomega parameter in Eventually/Consistently blocks

**THE RULE:** Any Eventually or Consistently block that makes assertions MUST use the Gomega parameter `g` and call `g.Expect()`, never `Expect()`.

### Why This Causes Flakes:

When `Expect()` is used without the Gomega parameter inside Eventually/Consistently blocks, assertion failures cause **panics** instead of allowing the block to retry. This completely defeats the purpose of Eventually/Consistently polling and causes intermittent test failures.

### Detection Method:

1. Use Grep to find all instances of `Eventually(func\(\))` and `Consistently(func\(\))` (without `g Gomega` parameter)
2. For each finding, use Read to examine the surrounding context (10-15 lines after the Eventually/Consistently line)
3. Check if the block contains any bare `Expect(` calls (not `g.Expect`)
4. Report each violation with file path, line number, and code snippet

### Bad vs Good Examples:

**❌ BAD (Flaky):**
```go
Consistently(func() []kuadrantdnsv1alpha1.DNSRecord {
    dnsRecords := kuadrantdnsv1alpha1.DNSRecordList{}
    err := k8sClient.List(ctx, &dnsRecords, client.InNamespace(ns))
    Expect(err).ToNot(HaveOccurred())  // ❌ PANIC on failure!
    return dnsRecords.Items
}, time.Second*15, time.Second).Should(BeEmpty())
```

**✅ GOOD (Reliable):**
```go
Consistently(func(g Gomega) []kuadrantdnsv1alpha1.DNSRecord {
    dnsRecords := kuadrantdnsv1alpha1.DNSRecordList{}
    err := k8sClient.List(ctx, &dnsRecords, client.InNamespace(ns))
    g.Expect(err).ToNot(HaveOccurred())  // ✅ Returns error, retries
    return dnsRecords.Items
}, time.Second*15, time.Second).Should(BeEmpty())
```

## Pattern 2: Get-Modify-Update Antipattern (Must Use Patch)

**THE RULE:** Never use Get-Modify-Update pattern. Use Patch instead, choosing the right patch strategy:
- **Server-Side Apply (SSA)** `client.Apply`: For adding/updating fields (preferred for simple cases)
- **MergeFrom Patch** `client.MergeFrom`: For removing fields or complex updates requiring current state

### Why This Causes Flakes:

The Get-Modify-Update pattern is susceptible to race conditions:
1. Get the resource (receives version N)
2. Modify the resource locally
3. Update (fails if another process updated to version N+1)

This results in "resource version conflict" errors and flaky tests. Even with Eventually, Update is less safe than Patch because Update replaces the entire resource, while Patch only modifies changed fields.

### Detection Method:

1. **Search for Update calls:** Use Grep to find `.Update\(ctx,` pattern
   - This catches: `k8sClient.Update(ctx,`, `testClient().Update(ctx,`, etc.
   - Do NOT search for `Status().Update` here (that's Pattern 3)

2. **For each Update finding:**
   - Use Read to examine the preceding 30 lines
   - Look for TWO things:
     a) `.Get\(ctx,` or `.Get\(` anywhere in those 30 lines (indicates Get-Modify pattern)
     b) `Eventually\(func` or `Consistently\(func` in those 30 lines (indicates proper wrapping)

3. **Classify the violation:**
   - **Found .Get() + NO Eventually:** CRITICAL - Must use Patch (no Eventually needed with SSA)
   - **Found .Get() + Has Eventually + Uses .Update():** HIGH - Should use Patch instead of Update
   - **No .Get() found:** Not a violation (skip this Update call)

4. **Determine recommended fix based on operation:**
   - Examine what's being modified in the code
   - **If setting fields to nil or removing:** Recommend MergeFrom Patch
   - **If only adding/updating fields (especially Kuadrant CR):** Recommend Server-Side Apply
   - **If unsure or complex:** Recommend MergeFrom Patch (works for all cases)

### Bad vs Good Examples:

**❌ BAD Example 1 (Get-Update removing a field):**
```go
Eventually(func(g Gomega) {
    err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
    g.Expect(err).NotTo(HaveOccurred())
    dnsPolicy.Spec.LoadBalancing = nil  // Removing a field
    err = k8sClient.Update(ctx, dnsPolicy)  // ❌ Resource version conflict!
    g.Expect(err).To(Succeed())
}, tests.TimeoutMedium, time.Second).Should(Succeed())
```

**✅ GOOD Example 1 (MergeFrom for removing fields):**
```go
Eventually(func(g Gomega) {
    err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsPolicy), dnsPolicy)
    g.Expect(err).NotTo(HaveOccurred())
    patch := client.MergeFrom(dnsPolicy.DeepCopy())  // Create patch AFTER Get
    dnsPolicy.Spec.LoadBalancing = nil  // Removing a field
    err = k8sClient.Patch(ctx, dnsPolicy, patch)  // ✅ Only patches changed fields!
    g.Expect(err).To(Succeed())
}, tests.TimeoutMedium, time.Second).Should(Succeed())
```

**❌ BAD Example 2 (Get-Update adding a field - NOT in Eventually):**
```go
kuadrantObj := &kuadrantv1beta1.Kuadrant{}
g.Expect(k8sClient.Get(ctx, kuadrantKey, kuadrantObj)).To(Succeed())
kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: true}  // Adding a field
g.Expect(k8sClient.Update(ctx, kuadrantObj)).To(Succeed())  // ❌❌ CRITICAL!
```

**❌ BAD Example 3 (Eventually(Get) but Update OUTSIDE Eventually):**
```go
Eventually(testClient().Get).WithContext(ctx).WithArguments(kuadrantKey, kuadrantObj).Should(Succeed())
kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: true}
Expect(testClient().Update(ctx, kuadrantObj)).To(Succeed())  // ❌❌ CRITICAL! Update is OUTSIDE Eventually!
```

**✅ GOOD Example 2 (Server-Side Apply for adding/updating - preferred for Kuadrant CR):**
```go
// No Get needed - construct the desired state directly
// No Eventually needed - SSA handles conflicts automatically
patch := &kuadrantv1beta1.Kuadrant{
    TypeMeta: metav1.TypeMeta{
        APIVersion: kuadrantv1beta1.GroupVersion.String(),
        Kind:       "Kuadrant",
    },
    ObjectMeta: metav1.ObjectMeta{
        Name:      "kuadrant-sample",
        Namespace: kuadrantInstallationNS,
    },
    Spec: kuadrantv1beta1.KuadrantSpec{
        MTLS: &kuadrantv1beta1.MTLS{Enable: true},
    },
}
Expect(k8sClient.Patch(ctx, patch, client.Apply,
    client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())  // ✅ Simpler!
```

**✅ ALSO GOOD (MergeFrom works too, but requires Eventually):**
```go
Eventually(func(g Gomega) {
    kuadrantObj := &kuadrantv1beta1.Kuadrant{}
    g.Expect(k8sClient.Get(ctx, kuadrantKey, kuadrantObj)).To(Succeed())
    patch := client.MergeFrom(kuadrantObj.DeepCopy())
    kuadrantObj.Spec.MTLS = &kuadrantv1beta1.MTLS{Enable: true}
    g.Expect(k8sClient.Patch(ctx, kuadrantObj, patch)).To(Succeed())
}).WithContext(ctx).Should(Succeed())  // ✅ Safe but more verbose
```

## Pattern 3: Status Updates Must Use Patch

**THE RULE:** Status updates MUST use `Status().Patch()` with `client.MergeFrom`, never `Status().Update()`.

### Why This Causes Flakes:

Status updates using `Update()` are prone to conflicts because:
1. Multiple controllers may update different status fields simultaneously
2. Status updates happen frequently during reconciliation
3. Using `Update()` overwrites the entire status, losing concurrent changes from other controllers

### Detection Method:

1. Use Grep to find all instances of `Status\(\)\.Update\(`
2. **ALL findings are violations** - Status().Update() should NEVER be used in tests
3. For each finding:
   - Use Read to examine the preceding 30 lines
   - Check if wrapped in Eventually (look for `Eventually\(func` in those 30 lines)
   - If NOT in Eventually: Flag as additional violation (must wrap in Eventually)
4. Report with MergeFrom Patch recommendation

### Bad vs Good Examples:

**❌ BAD (Flaky - can lose concurrent status updates):**
```go
Eventually(func(g Gomega) {
    g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)).To(Succeed())
    gateway.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
        {Type: ptr.To(gatewayapiv1.IPAddressType), Value: "1.2.3.4"},
    }
    g.Expect(k8sClient.Status().Update(ctx, gateway)).To(Succeed())  // ❌ Overwrites entire status!
}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())
```

**✅ GOOD (Reliable - only patches changed fields):**
```go
Eventually(func(g Gomega) {
    g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gateway), gateway)).To(Succeed())
    patch := client.MergeFrom(gateway.DeepCopy())  // Create patch AFTER Get
    gateway.Status.Addresses = []gatewayapiv1.GatewayStatusAddress{
        {Type: ptr.To(gatewayapiv1.IPAddressType), Value: "1.2.3.4"},
    }
    g.Expect(k8sClient.Status().Patch(ctx, gateway, patch)).To(Succeed())  // ✅ Only patches Addresses!
}, tests.TimeoutMedium, tests.RetryIntervalMedium).Should(Succeed())
```

## Execution Instructions:

### Phase 1: Pattern 1 - Expect without Gomega Parameter

1. **Search for Eventually/Consistently blocks without Gomega parameter:**
   - Use Grep with pattern: `Eventually\(func\(\)` (finds blocks starting with `Eventually(func()`)
   - Use Grep with pattern: `Consistently\(func\(\)` (finds blocks starting with `Consistently(func()`)
   - These patterns match blocks WITHOUT `g Gomega` parameter

2. **For EACH finding:**
   - Use Read to examine 10-15 lines after the match
   - Look for bare `Expect(` calls (not preceded by `g.`)
   - If found, this is a violation

3. **Report violations:**
   - Include file path and line number
   - Show code snippet with the problematic `Expect()` call
   - Recommend adding `g Gomega` parameter and using `g.Expect()`

### Phase 2: Pattern 2 - Get-Modify-Update (Must Use Patch)

1. **Search for Update calls:**
   - Use Grep with pattern: `\.Update\(ctx,`
   - This will find both regular updates AND status updates

2. **For EACH Update finding:**
   - **First, check if it's a Status update:**
     - If the line contains `Status().Update`, skip it (will be handled by Phase 3)
     - If not, proceed with analysis below
   - Use Read to examine the preceding 30 lines from the Update call
   - Search for TWO indicators:
     - **Indicator A:** `.Get` anywhere in those 30 lines (matches both `.Get(` and `.Get)`)
       - This catches: `k8sClient.Get(`, `testClient().Get(`, AND `Eventually(testClient().Get)`
     - **Indicator B:** `Eventually\(func` or `Consistently\(func` in those 30 lines
       - This indicates the Update is inside an Eventually block
       - Note: `Eventually(testClient().Get)` does NOT count as wrapping (no func parameter)

3. **Classification logic:**
   - If NO `.Get` found → Skip (not a Get-Modify-Update pattern)
   - If `.Get` found + NO `Eventually\(func` → **CRITICAL** violation
     - Includes `Eventually(testClient().Get)` pattern (Update is OUTSIDE Eventually)
   - If `.Get` found + Has `Eventually\(func` → **HIGH** violation
     - Update is inside Eventually but should use Patch

4. **Determine recommended fix - analyze the modification:**
   - Read the code between Get and Update to see what's being modified
   - Look for these patterns:
     - `= nil` → Field removal → **Use MergeFrom**
     - `= ""` or `= []` → Empty value → **Use MergeFrom**
     - `= &SomeType{...}` → Adding/updating field → **Consider SSA**
     - Multiple field changes → **Consider MergeFrom for safety**

5. **Provide recommendation:**
   - **For Kuadrant CR (small spec, adding/updating):**
     ```go
     // Server-Side Apply (SSA) - Preferred for simple add/update
     patch := &kuadrantv1beta1.Kuadrant{
         TypeMeta: metav1.TypeMeta{
             APIVersion: kuadrantv1beta1.GroupVersion.String(),
             Kind:       "Kuadrant",
         },
         ObjectMeta: metav1.ObjectMeta{Name: "...", Namespace: "..."},
         Spec: kuadrantv1beta1.KuadrantSpec{
             FieldToUpdate: value,
         },
     }
     Expect(k8sClient.Patch(ctx, patch, client.Apply,
         client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
     ```

   - **For removing fields or complex updates:**
     ```go
     // MergeFrom Patch - Required for field removal
     Eventually(func(g Gomega) {
         g.Expect(k8sClient.Get(ctx, key, obj)).To(Succeed())
         patch := client.MergeFrom(obj.DeepCopy())
         obj.Spec.Field = nil  // or any update
         g.Expect(k8sClient.Patch(ctx, obj, patch)).To(Succeed())
     }).WithContext(ctx).Should(Succeed())
     ```

   - **If unsure:** Recommend MergeFrom (works for all cases)

### Phase 3: Pattern 3 - Status Updates

1. **Search for Status Update calls:**
   ```
   Use Grep with pattern: `Status\(\)\.Update\(`
   ```

2. **For EACH finding (all are violations):**
   - Use Read to examine the preceding 30 lines
   - Check for `Eventually\(func` in those 30 lines
   - Classification:
     - If NO Eventually found → **CRITICAL** (two violations: missing Eventually + wrong method)
     - If Eventually found → **HIGH** (one violation: wrong method)

3. **Recommended fix:**
   - ALWAYS show Status().Patch() with MergeFrom pattern:
   ```go
   Eventually(func(g Gomega) {
       g.Expect(k8sClient.Get(ctx, key, obj)).To(Succeed())
       patch := client.MergeFrom(obj.DeepCopy())
       obj.Status.Field = value
       g.Expect(k8sClient.Status().Patch(ctx, obj, patch)).To(Succeed())
   }).WithContext(ctx).Should(Succeed())
   ```

### Phase 4: Reporting

Provide a comprehensive report with:

**1. Summary Statistics:**
- Total violations found
- Files affected
- Breakdown by pattern type:
  - Pattern 1 (Bare Expect): X violations
  - Pattern 2 (Get-Update): Y violations (Z critical, W high)
  - Pattern 3 (Status Update): N violations
- Total CRITICAL: X (must fix immediately)
- Total HIGH: Y (should fix)
- Total MEDIUM: Z (recommended fix)

**2. Detailed Findings:** For each violation, include:
```
Pattern X: [SEVERITY] Description
File: path/to/file.go:LineNumber
Code snippet (5-10 lines showing the problem)

❌ Problem:
[Explanation of what's wrong]

✅ Recommended Fix:
[Code snippet showing correct pattern]
```

**3. Organized by Severity:**

**CRITICAL Violations (Fix Immediately):**
- Get-Modify-Update NOT in Eventually blocks
- Status updates NOT in Eventually blocks

**HIGH Violations (Should Fix):**
- Get-Update in Eventually blocks (should use Patch)
- Status().Update() in Eventually blocks (should use Status().Patch())

**MEDIUM Violations (Recommended):**
- Bare Expect() in Eventually/Consistently blocks

## Important Notes:

- **Focus:** Only analyze test files in the `tests/` directory
- **Client names:** Code uses both `k8sClient` (common tests) and `testClient()` (other tests) - detect both
- **Be thorough:** Missing a flaky test is worse than a false positive
- **Context matters:** Read 30 lines before Update/Status().Update calls to properly classify
- **Status updates:** Status().Update() should ALWAYS use Status().Patch() with MergeFrom - no exceptions
- **Spec updates - choose the right tool:**
  - **Server-Side Apply (SSA):** Preferred for Kuadrant CR and simple add/update operations
    - Advantages: No Get needed, no Eventually needed, simpler code
    - Limitations: Cannot remove fields (set to nil), requires TypeMeta
  - **MergeFrom Patch:** Required for removing fields or complex updates
    - Advantages: Works for all operations (add, update, remove), no TypeMeta needed
    - Must be in Eventually block with Get
- **Eventually wrapper:** MergeFrom patterns MUST be in Eventually blocks; SSA doesn't need Eventually
- **Gomega parameter:** Eventually/Consistently blocks making assertions MUST use `g Gomega` parameter
- **Manual verification:** Some edge cases may need manual review to avoid false positives

## Quick Reference: Common Violations

| Violation | Severity | Recommended Fix |
|-----------|----------|-----------------|
| Get-Update outside Eventually | CRITICAL | Use SSA (if adding/updating) or MergeFrom in Eventually (if removing) |
| Get-Update inside Eventually | HIGH | Change to Patch: SSA (simple) or MergeFrom (complex/remove) |
| Status().Update() anywhere | HIGH | Change to Status().Patch() with MergeFrom (always) |
| Bare Expect() in Eventually | MEDIUM | Add `g Gomega` parameter, use `g.Expect()` |

## Decision Tree: Which Patch Type to Use?

```
Is it a Status update?
├─ YES → Use Status().Patch() with MergeFrom (always)
└─ NO → Is it removing a field (= nil) or setting empty value?
    ├─ YES → Use MergeFrom Patch in Eventually block
    └─ NO → Is it Kuadrant CR or simple add/update?
        ├─ YES → Prefer Server-Side Apply (SSA) - simpler, no Eventually needed
        └─ NO → Use MergeFrom Patch in Eventually block (safer for complex cases)
```
