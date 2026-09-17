"""Run-scoped Agents SDK adapter and simulated commerce workflow."""
import asyncio
import copy
import json
import pathlib
import tempfile
import uuid
from datetime import datetime, timedelta, timezone

import httpx
from agents import Agent, ModelSettings, RunConfig, Runner, handoff
from agents.items import ModelResponse
from agents.models.interface import Model
from agents.mcp import MCPServerStdio
from agents.tracing import TracingProcessor
from agents.usage import Usage
from openai.types.responses import ResponseFunctionToolCall, ResponseOutputMessage, ResponseOutputText


def timestamp(minutes=60):
    return (datetime.now(timezone.utc) + timedelta(minutes=minutes)).isoformat()


class LocalTrace(TracingProcessor):
    """Store correlation identifiers only. Install explicitly in the demo process."""
    def __init__(self):
        self.events = []

    def on_trace_start(self, trace):
        self.events.append({"event": "trace", "trace_id": trace.trace_id})

    def on_span_start(self, span):
        self.events.append({"event": "span", "trace_id": span.trace_id,
                            "span_id": span.span_id, "type": span.span_data.type})

    def on_trace_end(self, trace):
        pass

    def on_span_end(self, span):
        pass

    def shutdown(self):
        pass

    def force_flush(self):
        pass


class Gateway(MCPServerStdio):
    def __init__(self, workflow, stage, **kwargs):
        super().__init__(**kwargs)
        self.workflow, self.stage = workflow, stage
        self.lock = asyncio.Lock()

    async def call_tool(self, tool_name, arguments=None, meta=None):
        async with self.lock:
            return await self._call_once(tool_name, arguments, meta)

    async def _call_once(self, tool_name, arguments, meta):
        expected = self.workflow.arguments(self.stage)
        if arguments != expected:
            raise RuntimeError("Tool arguments differ from this workflow's authorized request")
        if self.stage in self.workflow.receipts:
            raise RuntimeError("This workflow stage has already executed")
        result = await super().call_tool(tool_name, arguments, meta)
        if result.is_error:
            raise RuntimeError("HandoffGuard denied the tool call or upstream execution failed")
        receipt = result.structured_content
        required = {"support": "order_id", "billing": "refund_id", "notification": "notification_id"}[self.stage]
        if not isinstance(receipt, dict) or not receipt.get(required) or receipt.get("order_id") != "48319":
            raise RuntimeError("Upstream returned no valid simulated receipt")
        if self.stage == "billing" and receipt.get("amount") != self.workflow.amount:
            raise RuntimeError("Refund receipt amount does not match the request")
        if self.stage == "notification" and receipt.get("refund_id") != expected["refund_id"]:
            raise RuntimeError("Notification receipt does not match the refund")
        self.workflow.receipts[self.stage] = receipt
        return result


class OfflineModel(Model):
    """Deterministic model; all orchestration and MCP execution still use the SDK."""
    def __init__(self, workflow, stage):
        self.workflow, self.stage, self.step = workflow, stage, 0

    async def get_response(self, system_instructions, input, model_settings, tools,
                           output_schema, handoffs, tracing, **kwargs):
        self.step += 1
        if self.step == 1:
            name, arguments = tools[0].name, self.workflow.arguments(self.stage)
        elif handoffs:
            name, arguments = handoffs[0].tool_name, {}
        else:
            return ModelResponse(output=[ResponseOutputMessage(id=uuid.uuid4().hex,
                type="message", role="assistant", status="completed",
                content=[ResponseOutputText(type="output_text", text="Simulated refund and notification complete.", annotations=[])])],
                usage=Usage(), response_id=None)
        return ModelResponse(output=[ResponseFunctionToolCall(type="function_call",
            name=name, arguments=json.dumps(arguments), call_id=uuid.uuid4().hex)],
            usage=Usage(), response_id=None)

    async def stream_response(self, *args, **kwargs):
        raise NotImplementedError("Offline demo supports Runner.run only")
        yield  # pragma: no cover


class Workflow:
    """One workflow instance per run; credentials never enter agent context."""
    def __init__(self, binary, base_url, token, *, amount=100, approve_demo_refund=False, model=None):
        if type(amount) is not int or amount <= 0:
            raise ValueError("amount must be a positive integer")
        url = httpx.URL(base_url)
        if url.scheme != "https" and not (url.scheme == "http" and url.host in {"localhost", "127.0.0.1", "::1"}):
            raise ValueError("Control API requires HTTPS or loopback HTTP")
        self.binary = str(pathlib.Path(binary).resolve())
        self.base_url, self.token = base_url, token
        self.amount, self.approve_demo_refund, self.model = amount, approve_demo_refund, model
        self.receipts, self.envelopes, self.sessions = {}, {}, []
        self.run_id = None

    async def __aenter__(self):
        self.directory = tempfile.TemporaryDirectory(prefix="hg-agents-")
        self.client = httpx.AsyncClient(base_url=self.base_url, timeout=20,
            headers={"Authorization": "Bearer " + self.token}, follow_redirects=False)
        return self

    async def __aexit__(self, *args):
        try:
            for stop, _ in self.sessions:
                stop.set()
            outcomes = await asyncio.gather(*(task for _, task in self.sessions), return_exceptions=True)
            if args[0] is None:
                for outcome in outcomes:
                    if isinstance(outcome, BaseException):
                        raise outcome
        finally:
            await self.client.aclose()
            self.directory.cleanup()

    async def api(self, method, path, body=None):
        response = await self.client.request(method, path, json=body)
        if response.status_code not in (200, 201):
            raise RuntimeError(f"HandoffGuard request denied or failed: HTTP {response.status_code}")
        return response.json()

    def arguments(self, stage):
        if stage == "support":
            return {"order_id": "48319"}
        if stage == "billing":
            return {"order_id": "48319", "amount": self.amount}
        return {"order_id": "48319", "refund_id": self.receipts["billing"]["refund_id"]}

    def child(self, parent_stage, stage):
        parent = self.envelopes[parent_stage]
        child = copy.deepcopy(parent)
        for key in ("id", "signature"):
            child.pop(key, None)
        child.update(issuer=parent["recipient"], recipient={"agent": stage + "-agent"},
            parent_envelope=parent["id"],
            allowed_actions=["refunds.create", "email.send"] if stage == "billing" else ["email.send"])
        child["delegation"]["current_depth"] += 1
        child["expires_at"] = (datetime.fromisoformat(parent["expires_at"].replace("Z", "+00:00")) - timedelta(seconds=60)).isoformat()
        return child

    async def connect(self, stage):
        tool, action = {"support": ("orders.get", "orders.read"),
                        "billing": ("refund.create", "refunds.create"),
                        "notification": ("email.send", "email.send")}[stage]
        config = pathlib.Path(self.directory.name) / (stage + ".json")
        config.write_text(json.dumps({"tools": {tool: {"action": action,
            "resources": {"orders": "/order_id"}, "data_classes": ["payment_metadata"]}}}))
        server = Gateway(self, stage, name=stage, cache_tools_list=True,
            use_structured_content=True, failure_error_function=None,
            max_retry_attempts=0, client_session_timeout_seconds=35,
            params={"command": self.binary, "args": ["gateway", "--config", str(config),
                "--agent", stage + "-agent", "--envelope", self.envelopes[stage]["id"],
                "--api-url", self.base_url, "--", self.binary, "demo-mcp"],
                "env": {"HANDOFFGUARD_API_TOKEN": self.token}})
        ready = asyncio.get_running_loop().create_future()
        stop = asyncio.Event()

        async def owner():
            try:
                async with server:
                    ready.set_result(server)
                    await stop.wait()
            except BaseException as error:
                if not ready.done():
                    ready.set_exception(error)
                else:
                    raise

        task = asyncio.create_task(owner())
        self.sessions.append((stop, task))
        return await ready

    async def transition(self, parent_stage, stage, agent):
        if parent_stage not in self.receipts or stage in self.envelopes:
            raise RuntimeError("Handoff requires a completed preceding stage and a fresh child")
        result = await self.api("POST", "/v1/envelopes/" + self.envelopes[parent_stage]["id"] + "/delegate",
                                {"child": self.child(parent_stage, stage)})
        self.envelopes[stage] = result["envelope"]
        if stage == "billing" and self.approve_demo_refund:
            await self.api("POST", "/v1/approvals", {"envelope_id": result["envelope"]["id"],
                "action": "refunds.create", "resource": "orders:48319", "arguments": self.arguments(stage),
                "approved_by": "demo-operator", "role": "refund_manager", "expires_at": timestamp(10)})
        agent.mcp_servers = [await self.connect(stage)]

    async def run(self):
        if self.run_id is not None:
            raise RuntimeError("Create a new Workflow for each run")
        result = await self.api("POST", "/v1/envelopes", {"envelope": {
            "version": "1", "issuer": {"agent": "demo-operator"}, "recipient": {"agent": "support-agent"},
            "purpose": "customer_refund", "policy_version": "refund-v1",
            "allowed_actions": ["orders.read", "refunds.create", "email.send"],
            "denied_actions": ["payments.export"], "resources": {"orders": ["48319"]},
            "data_classes": ["payment_metadata"], "approvals": [{"condition": "has(refund.amount) && refund.amount > 500", "required_role": "refund_manager"}],
            "delegation": {"max_depth": 2, "current_depth": 0, "may_expand_authority": False},
            "expires_at": timestamp()}})
        self.run_id, self.envelopes["support"] = result["run_id"], result["envelope"]
        agents = {}
        for stage in ("support", "billing", "notification"):
            agents[stage] = Agent(name=stage, model=self.model or OfflineModel(self, stage),
                instructions=f"You are {stage}. Call your one MCP tool once, then hand off if available. Order is 48319; refund amount is {self.amount}. Notification must use the refund_id from the successful refund tool result. Never claim completion before tool success.",
                model_settings=ModelSettings(parallel_tool_calls=False),
                mcp_config={"include_server_in_tool_names": True})
        async def billing(ctx):
            await self.transition("support", "billing", agents["billing"])
        async def notification(ctx):
            await self.transition("billing", "notification", agents["notification"])
        agents["support"].handoffs = [handoff(agents["billing"], on_handoff=billing)]
        agents["billing"].handoffs = [handoff(agents["notification"], on_handoff=notification)]
        agents["support"].mcp_servers = [await self.connect("support")]
        result = await Runner.run(agents["support"], "Process the simulated refund and notify the customer.",
            max_turns=10, run_config=RunConfig(workflow_name="HandoffGuard commerce demo",
                group_id=self.run_id, trace_include_sensitive_data=False))
        if "notification" not in self.receipts:
            raise RuntimeError("Agent stopped before completing the workflow")
        return result
