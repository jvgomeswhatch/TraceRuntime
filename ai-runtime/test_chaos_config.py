import os
import importlib
import time
import threading

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient

TOKEN = "a" * 32


def _build_app(chaos_enabled: bool):
    """Build a fresh FastAPI app with chaos_config reloaded."""
    os.environ["CHAOS_ENABLED"] = str(chaos_enabled).lower()
    os.environ["INTERNAL_TOKEN"] = TOKEN

    import chaos_config
    importlib.reload(chaos_config)

    app = FastAPI()
    if chaos_config.CHAOS_ENABLED:
        chaos_config.validate_chaos_startup()
        app.include_router(chaos_config.chaos_router)
        chaos_config.chaos_state.reset()

    return app, chaos_config


# --- CHAOS_ENABLED=false: endpoints return 404 ---

class TestChaosDisabled:
    @pytest.fixture(autouse=True)
    def setup(self):
        self.app, _ = _build_app(chaos_enabled=False)
        self.client = TestClient(self.app)

    def test_get_config_404(self):
        r = self.client.get("/internal/chaos/config", headers={"X-Internal-Token": TOKEN})
        assert r.status_code == 404

    def test_post_config_404(self):
        r = self.client.post("/internal/chaos/config", json={"delay_seconds": 5}, headers={"X-Internal-Token": TOKEN})
        assert r.status_code == 404

    def test_reset_404(self):
        r = self.client.post("/internal/chaos/reset", headers={"X-Internal-Token": TOKEN})
        assert r.status_code == 404


# --- CHAOS_ENABLED=true, invalid token: 403 ---

class TestChaosInvalidToken:
    @pytest.fixture(autouse=True)
    def setup(self):
        self.app, _ = _build_app(chaos_enabled=True)
        self.client = TestClient(self.app)

    def test_get_config_forbidden(self):
        r = self.client.get("/internal/chaos/config", headers={"X-Internal-Token": "wrong"})
        assert r.status_code == 403

    def test_post_config_forbidden(self):
        r = self.client.post("/internal/chaos/config", json={"delay_seconds": 5}, headers={"X-Internal-Token": "wrong"})
        assert r.status_code == 403

    def test_reset_forbidden(self):
        r = self.client.post("/internal/chaos/reset", headers={"X-Internal-Token": "wrong"})
        assert r.status_code == 403

    def test_no_token_forbidden(self):
        r = self.client.get("/internal/chaos/config")
        assert r.status_code == 403


# --- CHAOS_ENABLED=true, valid token: delay applied and reset clears it ---

class TestChaosValidToken:
    @pytest.fixture(autouse=True)
    def setup(self):
        self.app, self.chaos = _build_app(chaos_enabled=True)
        self.client = TestClient(self.app)
        self.headers = {"X-Internal-Token": TOKEN}

    def test_get_config_defaults(self):
        r = self.client.get("/internal/chaos/config", headers=self.headers)
        assert r.status_code == 200
        data = r.json()
        assert data["delay_seconds"] == 0.0
        assert data["failure_rate"] == 0.0

    def test_set_delay(self):
        r = self.client.post("/internal/chaos/config", json={"delay_seconds": 10}, headers=self.headers)
        assert r.status_code == 200
        assert r.json()["delay_seconds"] == 10

        snapshot = self.chaos.chaos_state.snapshot()
        assert snapshot["delay_seconds"] == 10

    def test_reset_clears_delay(self):
        self.client.post("/internal/chaos/config", json={"delay_seconds": 15}, headers=self.headers)
        assert self.chaos.chaos_state.snapshot()["delay_seconds"] == 15

        r = self.client.post("/internal/chaos/reset", headers=self.headers)
        assert r.status_code == 200
        assert r.json()["delay_seconds"] == 0.0
        assert self.chaos.chaos_state.snapshot()["delay_seconds"] == 0.0

    def test_delay_is_applied(self):
        self.chaos.chaos_state.update(delay_seconds=0.2)

        start = time.monotonic()
        self.chaos.chaos_state.apply_delay()
        elapsed = time.monotonic() - start

        assert elapsed >= 0.15

    def test_state_is_in_memory_only(self):
        self.chaos.chaos_state.update(delay_seconds=99)
        _, fresh_chaos = _build_app(chaos_enabled=True)
        assert fresh_chaos.chaos_state.snapshot()["delay_seconds"] == 0.0

    def test_state_is_thread_safe(self):
        results = []

        def writer():
            for i in range(100):
                self.chaos.chaos_state.update(delay_seconds=float(i))

        def reader():
            for _ in range(100):
                s = self.chaos.chaos_state.snapshot()
                results.append(s["delay_seconds"])

        t1 = threading.Thread(target=writer)
        t2 = threading.Thread(target=reader)
        t1.start()
        t2.start()
        t1.join()
        t2.join()

        assert len(results) == 100
        assert all(isinstance(v, float) for v in results)


# --- Startup validation ---

class TestStartupValidation:
    def test_short_token_raises(self):
        os.environ["CHAOS_ENABLED"] = "true"
        os.environ["INTERNAL_TOKEN"] = "short"

        import chaos_config
        importlib.reload(chaos_config)

        with pytest.raises(RuntimeError, match="at least 32 characters"):
            chaos_config.validate_chaos_startup()

    def test_disabled_token_raises(self):
        os.environ["CHAOS_ENABLED"] = "true"
        os.environ["INTERNAL_TOKEN"] = "disabled"

        import chaos_config
        importlib.reload(chaos_config)

        with pytest.raises(RuntimeError, match="at least 32 characters"):
            chaos_config.validate_chaos_startup()
