"""Integration tests require the temporary server started by smoke-agents.py."""
import asyncio
import json
import os
import unittest
from agents.tracing import set_trace_processors
from workflow import LocalTrace, Workflow, OfflineModel
from agents.exceptions import AgentsException


class ExpandedWorkflow(Workflow):
    def child(self, parent_stage, stage):
        child = super().child(parent_stage, stage)
        child['allowed_actions'].append('payments.export')
        return child


class WaitingModel(OfflineModel):
    def __init__(self):
        self.started = asyncio.Event()

    async def get_response(self, *args, **kwargs):
        self.started.set()
        await asyncio.Future()


class RepeatingModel(OfflineModel):
    async def get_response(self, *args, **kwargs):
        self.step = 0
        return await super().get_response(*args, **kwargs)


class WorkflowTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.traces = LocalTrace()
        set_trace_processors([self.traces])

    def workflow(self, cls=Workflow, **kwargs):
        return cls(os.environ['HANDOFFGUARD_BINARY'], os.environ['HANDOFFGUARD_SERVER_URL'],
                   os.environ['HANDOFFGUARD_API_TOKEN'], **kwargs)

    async def test_complete_chain_and_trace(self):
        async with self.workflow() as workflow:
            await workflow.run()
            self.assertEqual(set(workflow.receipts), {'support', 'billing', 'notification'})
            chain = await workflow.api('GET', '/v1/runs/' + workflow.run_id + '/chain')
            self.assertEqual(len(chain['envelopes']), 3)
            self.assertEqual(len(chain['handoffs']), 2)
            self.assertEqual(len(chain['actions']), 3)
            self.assertEqual(workflow.envelopes['notification']['allowed_actions'], ['email.send'])
            audit = await workflow.api('POST', '/v1/audit/' + workflow.run_id + '/verify')
            self.assertEqual(audit['status'], 'VALID')
        self.assertTrue(all(task.done() for _, task in workflow.sessions))
        self.assertEqual(sum(e.get('type') == 'handoff' for e in self.traces.events), 2)
        self.assertNotIn(os.environ['HANDOFFGUARD_API_TOKEN'], json.dumps(self.traces.events))

    async def test_missing_approval_stops_before_notification(self):
        async with self.workflow(amount=825) as workflow:
            with self.assertRaisesRegex(AgentsException, 'denied the tool call'):
                await workflow.run()
            self.assertEqual(set(workflow.receipts), {'support'})
            self.assertNotIn('notification', workflow.envelopes)
            chain = await workflow.api('GET', '/v1/runs/' + workflow.run_id + '/chain')
            self.assertEqual(len(chain['actions']), 2)
            self.assertEqual(len(chain['envelopes']), 2)
        self.assertTrue(all(task.done() for _, task in workflow.sessions))

    async def test_operator_approval(self):
        async with self.workflow(amount=825, approve_demo_refund=True) as workflow:
            await workflow.run()
            self.assertEqual(workflow.receipts['billing']['amount'], 825)
            self.assertIn('notification', workflow.receipts)

    async def test_denied_handoff_never_starts_child(self):
        async with self.workflow(ExpandedWorkflow) as workflow:
            with self.assertRaisesRegex(RuntimeError, '403'):
                await workflow.run()
            self.assertEqual(set(workflow.envelopes), {'support'})
            self.assertEqual(len(workflow.sessions), 1)
            chain = await workflow.api('GET', '/v1/runs/' + workflow.run_id + '/chain')
            self.assertEqual(len(chain['envelopes']), 1)
            self.assertEqual(len(chain['actions']), 1)
            self.assertEqual(len(chain['handoffs']), 1)

    async def test_handoff_before_tool_success_is_blocked(self):
        model = OfflineModel(None, 'support')
        model.step = 1
        async with self.workflow(model=model) as workflow:
            with self.assertRaisesRegex(RuntimeError, 'completed preceding stage'):
                await workflow.run()
            chain = await workflow.api('GET', '/v1/runs/' + workflow.run_id + '/chain')
            self.assertEqual(len(chain['actions']), 0)
            self.assertEqual(len(chain['handoffs']), 0)
            self.assertEqual(set(workflow.envelopes), {'support'})

    async def test_repeated_tool_does_not_reach_gateway(self):
        async with self.workflow() as workflow:
            workflow.model = RepeatingModel(workflow, 'support')
            with self.assertRaisesRegex(AgentsException, 'already executed'):
                await workflow.run()
            chain = await workflow.api('GET', '/v1/runs/' + workflow.run_id + '/chain')
            self.assertEqual(len(chain['actions']), 1)

    async def test_cancellation_closes_gateway_owners(self):
        model = WaitingModel()
        workflow = self.workflow(model=model)
        async def run():
            async with workflow:
                await workflow.run()
        task = asyncio.create_task(run())
        try:
            await asyncio.wait_for(model.started.wait(), timeout=20)
        finally:
            task.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await task
        self.assertTrue(workflow.sessions)
        self.assertTrue(all(owner.done() and not owner.cancelled() for _, owner in workflow.sessions))


if __name__ == '__main__':
    unittest.main()
