#!/usr/bin/env python3
"""Concurrent smoke test for synchronous image generation.

Usage:
  python scripts/test_sync_image_concurrency.py \
    --base-url http://localhost:3000 \
    --api-key sk-xxx \
    --concurrency 5 \
    --requests 10

  python scripts/test_sync_image_concurrency.py \
    --base-url http://localhost:3000 \
    --api-key sk-xxx \
    --submit-jobs
"""

from __future__ import annotations

import argparse
import concurrent.futures
import json
import time
import urllib.error
import urllib.request
from dataclasses import dataclass


@dataclass
class Result:
    index: int
    requested_n: int
    status: int
    elapsed: float
    retry_after: str
    returned_images: int | None
    job_id: str
    job_status: str
    body: str
    error: str


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Test synchronous image generation concurrency"
    )
    parser.add_argument(
        "--base-url", default="http://localhost:3000", help="new-api-new base URL"
    )
    parser.add_argument("--api-key", required=True, help="API key, e.g. sk-xxx")
    parser.add_argument("--model", default="gpt-image-2", help="image model name")
    parser.add_argument(
        "--prompt", default="生成二次元女生，女生穿着JK服装，女生身材很好，很活泼的样子，很美丽", help="prompt"
    )
    parser.add_argument("--size", default="1024x1024", help="image size")
    parser.add_argument("--n", type=int, default=1, help="number of images per request")
    parser.add_argument(
        "--concurrency", type=int, default=5, help="parallel worker count"
    )
    parser.add_argument("--requests", type=int, default=10, help="total request count")
    parser.add_argument(
        "--submit-jobs",
        action="store_true",
        help="submit /v1/images/generations/jobs instead of synchronous image requests",
    )
    parser.add_argument("--timeout", type=int, default=180, help="HTTP timeout seconds")
    parser.add_argument(
        "--body-chars", type=int, default=240, help="response body preview chars"
    )
    return parser.parse_args()


def call_image_generation(args: argparse.Namespace, index: int) -> Result:
    path = (
        "/v1/images/generations/jobs" if args.submit_jobs else "/v1/images/generations"
    )
    url = args.base_url.rstrip("/") + path
    payload = {
        "model": args.model,
        "prompt": f"{args.prompt} #{index}",
        "size": args.size,
        "n": args.n,
    }
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        method="POST",
        headers={
            "Authorization": f"Bearer {args.api_key}",
            "Content-Type": "application/json",
        },
    )
    started = time.perf_counter()
    try:
        with urllib.request.urlopen(req, timeout=args.timeout) as resp:
            body = resp.read().decode("utf-8", errors="replace")
            return Result(
                index=index,
                requested_n=args.n,
                status=resp.status,
                elapsed=time.perf_counter() - started,
                retry_after=resp.headers.get("Retry-After", ""),
                returned_images=count_returned_images(body),
                job_id=get_response_string(body, "id"),
                job_status=get_response_string(body, "status"),
                body=body[: args.body_chars],
                error="",
            )
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        return Result(
            index=index,
            requested_n=args.n,
            status=exc.code,
            elapsed=time.perf_counter() - started,
            retry_after=exc.headers.get("Retry-After", ""),
            returned_images=count_returned_images(body),
            job_id=get_response_string(body, "id"),
            job_status=get_response_string(body, "status"),
            body=body[: args.body_chars],
            error="",
        )
    except Exception as exc:  # noqa: BLE001 - test script should report all failures
        return Result(
            index=index,
            requested_n=args.n,
            status=0,
            elapsed=time.perf_counter() - started,
            retry_after="",
            returned_images=None,
            job_id="",
            job_status="",
            body="",
            error=str(exc),
        )


def main() -> None:
    args = parse_args()
    total_started = time.perf_counter()
    results: list[Result] = []
    with concurrent.futures.ThreadPoolExecutor(
        max_workers=args.concurrency
    ) as executor:
        futures = [
            executor.submit(call_image_generation, args, idx)
            for idx in range(1, args.requests + 1)
        ]
        for future in concurrent.futures.as_completed(futures):
            result = future.result()
            results.append(result)
            print_result(result)

    elapsed = time.perf_counter() - total_started
    print("\nSummary")
    print("-------")
    print(
        f"total_requests={args.requests} submit_jobs={args.submit_jobs} "
        f"concurrency={args.concurrency} total_elapsed={elapsed:.2f}s"
    )
    status_counts: dict[int, int] = {}
    for result in results:
        status_counts[result.status] = status_counts.get(result.status, 0) + 1
    for status in sorted(status_counts):
        label = "exception" if status == 0 else str(status)
        print(f"status_{label}={status_counts[status]}")


def print_result(result: Result) -> None:
    retry = f" retry_after={result.retry_after}" if result.retry_after else ""
    job = (
        f" job_id={result.job_id} job_status={result.job_status}"
        if result.job_id
        else ""
    )
    returned = (
        "unknown" if result.returned_images is None else str(result.returned_images)
    )
    print(
        f"#{result.index:03d} status={result.status} elapsed={result.elapsed:.2f}s "
        f"requested_n={result.requested_n} returned_images={returned}{job}{retry}"
    )
    if result.error:
        print(f"  error: {result.error}")
    elif result.body:
        preview = result.body.replace("\n", " ")
        print(f"  body: {preview}")


def count_returned_images(body: str) -> int | None:
    try:
        payload = json.loads(body)
    except json.JSONDecodeError:
        return None
    data = payload.get("data")
    if isinstance(data, list):
        return len(data)
    return None


def get_response_string(body: str, key: str) -> str:
    try:
        payload = json.loads(body)
    except json.JSONDecodeError:
        return ""
    value = payload.get(key)
    if isinstance(value, str):
        return value
    return ""


if __name__ == "__main__":
    main()
