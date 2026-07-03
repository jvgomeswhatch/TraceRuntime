import asyncio
import logging
import os
import threading
import time
from dataclasses import dataclass, field

from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse

log = logging.getLogger(__name__)

INTERNAL_TOKEN = os.getenv("CHAOS_INTERNAL_TOKEN", "disabled")
CHAOS_ENABLED = os.getenv("CHAOS_ENABLED", "false").lower() == "true"


@dataclass
class ChaosState:
    delay_seconds: float = 0.0
    failure_rate: float = 0.0
    timeout_rate: float = 0.0
    _lock: threading.Lock = field(default_factory=threading.Lock, repr=False)

    def update(self, delay_seconds: float = 0.0, failure_rate: float = 0.0, timeout_rate: float = 0.0):
        with self._lock:
            self.delay_seconds = delay_seconds
            self.failure_rate = failure_rate
            self.timeout_rate = timeout_rate

    def reset(self):
        with self._lock:
            self.delay_seconds = 0.0
            self.failure_rate = 0.0
            self.timeout_rate = 0.0

    def snapshot(self) -> dict:
        with self._lock:
            return {
                "delay_seconds": self.delay_seconds,
                "failure_rate": self.failure_rate,
                "timeout_rate": self.timeout_rate,
            }

    def apply_delay(self):
        with self._lock:
            delay = self.delay_seconds
        if delay > 0:
            log.info("chaos: applying delay", extra={"delay_seconds": delay})
            time.sleep(delay)

    async def apply_delay_async(self):
        with self._lock:
            delay = self.delay_seconds
        if delay > 0:
            log.info("chaos: applying async delay", extra={"delay_seconds": delay})
            await asyncio.sleep(delay)


chaos_state = ChaosState()
chaos_router = APIRouter(prefix="/internal/chaos", tags=["chaos"])


def _validate_token(request: Request) -> bool:
    token = request.headers.get("X-Internal-Token", "")
    return token == INTERNAL_TOKEN


@chaos_router.get("/config")
async def get_config(request: Request):
    if not _validate_token(request):
        return JSONResponse(status_code=403, content={"error": "forbidden"})
    return JSONResponse(content=chaos_state.snapshot())


@chaos_router.post("/config")
async def set_config(request: Request):
    if not _validate_token(request):
        return JSONResponse(status_code=403, content={"error": "forbidden"})
    body = await request.json()
    chaos_state.update(
        delay_seconds=body.get("delay_seconds", 0.0),
        failure_rate=body.get("failure_rate", 0.0),
        timeout_rate=body.get("timeout_rate", 0.0),
    )
    log.info("chaos: config updated", extra=chaos_state.snapshot())
    return JSONResponse(content=chaos_state.snapshot())


@chaos_router.post("/reset")
async def reset_config(request: Request):
    if not _validate_token(request):
        return JSONResponse(status_code=403, content={"error": "forbidden"})
    chaos_state.reset()
    log.info("chaos: config reset")
    return JSONResponse(content=chaos_state.snapshot())


def validate_chaos_startup():
    if CHAOS_ENABLED and (INTERNAL_TOKEN == "disabled" or len(INTERNAL_TOKEN) < 32):
        raise RuntimeError(
            "CHAOS_ENABLED=true requires CHAOS_INTERNAL_TOKEN with at least 32 characters"
        )
