-- Per-saga interval breakdown for the sagas currently in the disposable audit DB (orchestrator + payments schemas).
-- The load runner resets these tables before each run, so this describes the LAST run only.
with s as (
  select sg.id, sg.created_at acc, sg.updated_at done,
    max(case when st.step='RESERVE_STOCK' then st.finished_at end) rs_f,
    max(case when st.step='CREATE_ORDER' then st.started_at end) co_s, max(case when st.step='CREATE_ORDER' then st.finished_at end) co_f,
    max(case when st.step='TAKE_PAYMENT' then st.started_at end) tp_s, max(case when st.step='TAKE_PAYMENT' then st.finished_at end) tp_f,
    max(case when st.step='MARK_ORDER_PAID' then st.started_at end) mp_s, max(case when st.step='MARK_ORDER_PAID' then st.finished_at end) mp_f,
    max(case when st.step='CONFIRM_STOCK' then st.started_at end) cs_s, max(case when st.step='CONFIRM_STOCK' then st.finished_at end) cs_f
  from orchestrator.sagas sg join orchestrator.saga_steps st on st.saga_id=sg.id group by sg.id, sg.created_at, sg.updated_at
), j as (
  select s.*, pa.created_at pay_start, pa.capture_observed_at cap, pa.updated_at pay_rec from s left join payments.payment_attempts pa on pa.order_id=s.id
), iv(name,ord,ms) as (
  select '1 accept -> stock reserved',1, extract(epoch from rs_f-acc)*1000 from j union all
  select '2 reserved -> order created (CREATE_ORDER done)',2, extract(epoch from co_f-rs_f)*1000 from j union all
  select '3 order created -> TAKE_PAYMENT started',3, extract(epoch from tp_s-co_f)*1000 from j union all
  select '4a TAKE_PAYMENT started -> payment attempt row created',4, extract(epoch from pay_start-tp_s)*1000 from j union all
  select '4b attempt created -> capture observed (gateway pay delay + polling)',5, extract(epoch from cap-pay_start)*1000 from j union all
  select '5a capture observed -> TAKE_PAYMENT finished (result handling + reply delivery)',6, extract(epoch from tp_f-cap)*1000 from j union all
  select '5b TAKE_PAYMENT done -> MARK_ORDER_PAID done (order update)',7, extract(epoch from mp_f-tp_f)*1000 from j union all
  select '5c MARK_ORDER_PAID done -> CONFIRM_STOCK done (stock confirm)',8, extract(epoch from cs_f-mp_f)*1000 from j union all
  select '6 accept -> saga terminal (total)',9, extract(epoch from done-acc)*1000 from j
)
select name, count(ms) n, round(avg(ms)) avg_ms, round(percentile_cont(.5) within group(order by ms)) p50, round(percentile_cont(.95) within group(order by ms)) p95, round(max(ms)) max_ms
from iv group by name,ord order by ord;
