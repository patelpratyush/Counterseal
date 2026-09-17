"""Offline by default; --model enables explicit live OpenAI usage."""
import argparse
import asyncio
import json
import os

from agents.tracing import set_trace_processors
from workflow import LocalTrace, Workflow


async def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--model', help='Explicit OpenAI model name (requires OPENAI_API_KEY)')
    parser.add_argument('--amount', type=int, default=100)
    parser.add_argument('--approve-demo-refund', action='store_true',
                        help='Operator approval for this exact simulated refund; never a model tool')
    args = parser.parse_args()
    if args.model and not os.getenv('OPENAI_API_KEY'):
        parser.error('--model requires OPENAI_API_KEY')
    traces = LocalTrace()
    set_trace_processors([traces])
    async with Workflow(os.environ['HANDOFFGUARD_BINARY'], os.environ['HANDOFFGUARD_SERVER_URL'],
                        os.environ['HANDOFFGUARD_API_TOKEN'], amount=args.amount,
                        approve_demo_refund=args.approve_demo_refund, model=args.model) as workflow:
        result = await workflow.run()
        audit = await workflow.api('POST', '/v1/audit/' + workflow.run_id + '/verify')
        print(json.dumps({'run_id': workflow.run_id, 'output': result.final_output,
                          'receipts': workflow.receipts, 'audit': audit, 'traces': traces.events}, indent=2))


if __name__ == '__main__':
    asyncio.run(main())
