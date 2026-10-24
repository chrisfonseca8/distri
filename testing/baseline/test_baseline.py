import unittest, baseline as b


class Schedule(unittest.TestCase):
    def test_exactly_fifty_over_sixty_seconds_uniform_and_reproducible(self):
        products = [f"p{i:02d}" for i in range(50)]
        s = b.build_schedule(50, 60.0, products, 42)
        self.assertEqual(len(s), 50)
        self.assertAlmostEqual(s[1]["due"] - s[0]["due"], 1.2)
        self.assertAlmostEqual(s[-1]["due"], 58.8)
        self.assertEqual(sorted(r["product"] for r in s), products)       # each product exactly once
        self.assertEqual(s, b.build_schedule(50, 60.0, products, 42))      # same seed, same plan
        self.assertNotEqual([r["product"] for r in s], [r["product"] for r in b.build_schedule(50, 60.0, products, 43)])

    def test_more_arrivals_than_products_stays_balanced(self):
        s = b.build_schedule(10, 5.0, ["a", "b", "c"], 1)
        counts = [sum(1 for r in s if r["product"] == p) for p in "abc"]
        self.assertLessEqual(max(counts) - min(counts), 2)


class Accounting(unittest.TestCase):
    def rows(self):
        base = 100.0
        mk = lambda i, **kw: dict(i=i, scheduled=base + i, **kw)
        return [
            mk(0, dispatched=base + 0.01, status=202, order_id="o0", final="COMPLETED"),
            mk(1, dispatched=base + 1.20, status=202, order_id="o1", final="COMPENSATED"),
            mk(2, dispatched=base + 2.00, status=202, order_id="o2"),              # accepted, never terminal
            mk(3, dispatched=base + 3.00, status=409),                              # rejected
            mk(4, dispatched=base + 4.00, status=-1),                               # ambiguous timeout
            mk(5, dispatched=base + 5.00, status=0),                                # transport failure
            mk(6),                                                                  # never dispatched
        ]

    def test_counts_are_separate_and_honest(self):
        s = b.summarize(self.rows(), 10.0)
        self.assertEqual((s["scheduled"], s["dispatched"], s["accepted"]), (7, 6, 3))
        self.assertEqual((s["rejected"], s["ambiguous_timeouts"], s["failed_transport"]), (1, 1, 1))
        self.assertEqual((s["completed"], s["unresolved"]), (2, 1))
        self.assertEqual(s["final_status"], {"COMPENSATED": 1, "COMPLETED": 1})
        self.assertAlmostEqual(s["dispatch_lag_s"]["max"], 0.2, places=6)
        self.assertAlmostEqual(s["accepted_per_s"], 0.3)

    def test_completion_does_not_depend_on_acceptance_flag(self):
        rows = [dict(i=0, scheduled=0, dispatched=0, status=-1, final="COMPLETED")]   # timed out POST that landed anyway
        s = b.summarize(rows, 1.0)
        self.assertEqual((s["accepted"], s["completed"], s["completed_not_accepted"]), (0, 1, 1))


if __name__ == "__main__":
    unittest.main()
