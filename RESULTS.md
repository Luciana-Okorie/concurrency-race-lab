# Concurrency Test Notes

## 1. Naive Mode — 50 Workers

- **Workers:** 50
- **Debit amount:** ₦30
- **Starting balance:** ₦1,000
- **Maximum possible successful debits:** 33
- **Actual successful debits:** 50
- **Final balance:** -₦500
- **Result:** ❌ Overspending occurred.

### Observation

The race condition was successfully reproduced. More debits were processed than the account balance could legitimately support.

---

## 2. Safe Mode — 50 Workers

- **Workers:** 50
- **Debit amount:** ₦30
- **Starting balance:** ₦1,000
- **Maximum possible successful debits:** 33
- **Actual successful debits:** 33
- **Final balance:** ₦10
- **Tests:** Run 3 times with the same result
- **Result:** ✅ Correct.

### Observation

The number of successful debits and the final balance remained consistent with the starting balance every time.

---

## 3. Naive Mode — 500 Workers

- **Workers:** 500
- **Debit amount:** ₦5
- **Starting balance:** ₦1,000
- **Maximum possible successful debits:** 200
- **Actual successful debits:** 500
- **Final balance:** -₦1,500
- **Result:** ❌ Overspending occurred.

### Observation

The race condition became even more obvious under higher concurrency. All 500 workers were able to proceed even though only 200 debits were affordable.

---

## 4. Safe Mode — 500 Workers

- **Workers:** 500
- **Debit amount:** ₦5
- **Starting balance:** ₦1,000
- **Maximum possible successful debits:** 200
- **Actual successful debits:** 200
- **Final balance:** ₦0
- **Result:** ✅ Correct.

### Observation

The database correctly limited successful debits to the amount the account could afford.

---

# Why the Naive Approach Fails

The naive debit performs the balance check in application code:

1. Read the current balance.
2. Check whether there is enough money.
3. Wait briefly.
4. Write the new balance.

With concurrent requests, multiple goroutines can read the **same balance before any of them updates it**.

For example, with 500 workers:

- Starting balance = ₦1,000
- Each debit = ₦5
- Every worker can read ₦1,000.
- Each worker concludes that there is enough money.
- All 500 workers proceed.
- The account becomes **-₦1,500**.

This is a **race condition**.

---

# Why the Safe Approach Works

The safe implementation performs the balance check and debit as **one atomic SQL operation**:

```sql
UPDATE accounts
SET available = available - $1
WHERE available >= $1
RETURNING available;