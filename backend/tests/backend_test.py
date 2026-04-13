"""Kortex backend API tests"""
import os
import uuid

import pytest
import requests
from dotenv import dotenv_values

frontend_env = dotenv_values("/app/frontend/.env")
BASE_URL = (os.environ.get("REACT_APP_BACKEND_URL") or frontend_env.get("REACT_APP_BACKEND_URL")).rstrip("/")


@pytest.fixture(scope="session")
def creds():
    return {"email": "admin@kortex.dev", "password": "kortex2026"}


@pytest.fixture(scope="session")
def admin_token(creds):
    r = requests.post(f"{BASE_URL}/api/auth/login", json=creds, timeout=15)
    assert r.status_code == 200, r.text
    data = r.json()
    assert "access_token" in data
    assert data["user"]["email"] == creds["email"]
    return data["access_token"]


@pytest.fixture
def auth_headers(admin_token):
    return {"Authorization": f"Bearer {admin_token}"}


# ---------- Auth ----------
class TestAuth:
    def test_login_success(self, creds):
        r = requests.post(f"{BASE_URL}/api/auth/login", json=creds, timeout=15)
        assert r.status_code == 200
        d = r.json()
        assert "access_token" in d and "refresh_token" in d
        assert d["user"]["email"] == creds["email"]

    def test_login_wrong_password(self):
        r = requests.post(f"{BASE_URL}/api/auth/login",
                          json={"email": "admin@kortex.dev", "password": "wrongwrong"}, timeout=15)
        assert r.status_code in (401, 400)

    def test_login_lockout(self):
        # Use unique email to avoid affecting admin
        email = f"lockout-{uuid.uuid4().hex[:8]}@example.com"
        # Register then hit wrong password 5 times
        requests.post(f"{BASE_URL}/api/auth/register",
                      json={"email": email, "password": "correctpass123", "name": "Lockout Test"}, timeout=15)
        codes = []
        for _ in range(7):
            r = requests.post(f"{BASE_URL}/api/auth/login",
                              json={"email": email, "password": "wrongwrong"}, timeout=15)
            codes.append(r.status_code)
        assert 429 in codes, f"Expected a 429 lockout, got {codes}"

    def test_register_new_user(self):
        email = f"newuser-{uuid.uuid4().hex[:8]}@example.com"
        r = requests.post(f"{BASE_URL}/api/auth/register",
                          json={"email": email, "password": "supersecure1", "name": "New User"}, timeout=15)
        assert r.status_code in (200, 201), r.text
        d = r.json()
        assert "access_token" in d
        assert d["user"]["email"] == email

    def test_me(self, auth_headers, creds):
        r = requests.get(f"{BASE_URL}/api/auth/me", headers=auth_headers, timeout=15)
        assert r.status_code == 200
        assert r.json()["email"] == creds["email"]

    def test_me_no_auth(self):
        r = requests.get(f"{BASE_URL}/api/auth/me", timeout=15)
        assert r.status_code == 401


# ---------- Links ----------
class TestLinks:
    created_codes = []

    def test_alias_probe_available(self, auth_headers):
        alias = f"testalias-{uuid.uuid4().hex[:6]}"
        r = requests.get(f"{BASE_URL}/api/links/check-alias", params={"alias": alias},
                         headers=auth_headers, timeout=15)
        assert r.status_code == 200, r.text
        d = r.json()
        assert d.get("available") is True, d
        assert d.get("bloom_maybe") is False

    def test_alias_probe_taken(self, auth_headers):
        r = requests.get(f"{BASE_URL}/api/links/check-alias", params={"alias": "mergetree"},
                         headers=auth_headers, timeout=15)
        assert r.status_code == 200, r.text
        d = r.json()
        assert d.get("available") is False, d
        assert d.get("bloom_maybe") is True

    def test_shorten_link(self, auth_headers):
        payload = {"url": "https://example.com/foo?x=1"}
        r = requests.post(f"{BASE_URL}/api/links", json=payload, headers=auth_headers, timeout=15)
        assert r.status_code in (200, 201), r.text
        d = r.json()
        # accept several possible response shapes
        code = d.get("code") or (d.get("link") or {}).get("code")
        assert code, d
        TestLinks.created_codes.append(code)

    def test_shorten_with_alias_collision(self, auth_headers):
        r = requests.post(f"{BASE_URL}/api/links",
                          json={"url": "https://example.com", "alias": "mergetree"},
                          headers=auth_headers, timeout=15)
        assert r.status_code == 409, r.text

    def test_list_links(self, auth_headers):
        r = requests.get(f"{BASE_URL}/api/links", headers=auth_headers, timeout=15)
        assert r.status_code == 200
        d = r.json()
        # Accept list or {items:[]}
        items = d if isinstance(d, list) else d.get("items") or d.get("links") or []
        assert isinstance(items, list)
        assert len(items) > 0

    def test_redirect_hit(self):
        r = requests.get(f"{BASE_URL}/api/r/mergetree", allow_redirects=False, timeout=15)
        assert r.status_code in (301, 302), r.status_code
        assert "Location" in r.headers
        assert "clickhouse" in r.headers["Location"].lower() or r.headers["Location"].startswith("http")
        # cache header optional but expected
        cache_hdr = r.headers.get("x-kortex-cache", "").upper()
        assert cache_hdr in ("HIT", "MISS", "")

    def test_redirect_unknown(self):
        r = requests.get(f"{BASE_URL}/api/r/definitely-not-exist-xyz-{uuid.uuid4().hex[:6]}",
                         allow_redirects=False, timeout=15)
        assert r.status_code == 404

    def test_toggle_and_delete(self, auth_headers):
        # create link
        r = requests.post(f"{BASE_URL}/api/links",
                          json={"url": "https://example.com/toggle-test"},
                          headers=auth_headers, timeout=15)
        assert r.status_code in (200, 201)
        d = r.json()
        code = d.get("code") or (d.get("link") or {}).get("code")
        assert code
        # toggle
        rt = requests.post(f"{BASE_URL}/api/links/{code}/toggle", headers=auth_headers, timeout=15)
        assert rt.status_code in (200, 204), rt.text
        # disabled -> expect 410 on redirect
        rr = requests.get(f"{BASE_URL}/api/r/{code}", allow_redirects=False, timeout=15)
        assert rr.status_code in (410, 404), rr.status_code
        # delete
        rd = requests.delete(f"{BASE_URL}/api/links/{code}", headers=auth_headers, timeout=15)
        assert rd.status_code in (200, 204), rd.text


# ---------- Analytics ----------
class TestAnalytics:
    def test_overview(self, auth_headers):
        r = requests.get(f"{BASE_URL}/api/analytics/overview", headers=auth_headers, timeout=15)
        assert r.status_code == 200, r.text
        d = r.json()
        # returns dimensional breakdowns like browser/os/country plus totals
        assert isinstance(d, dict) and len(d) > 0

    def test_timeseries(self, auth_headers):
        r = requests.get(f"{BASE_URL}/api/analytics/mergetree/timeseries",
                         params={"range": "24h", "granularity": "hour"},
                         headers=auth_headers, timeout=15)
        assert r.status_code == 200, r.text
        d = r.json()
        assert "points" in d and isinstance(d["points"], list) and len(d["points"]) > 0

    def test_breakdown(self, auth_headers):
        r = requests.get(f"{BASE_URL}/api/analytics/mergetree/breakdown",
                         params={"dim": "country", "range": "24h"},
                         headers=auth_headers, timeout=15)
        assert r.status_code == 200, r.text

    def test_query_race(self, auth_headers):
        r = requests.get(f"{BASE_URL}/api/analytics/mergetree/race",
                         params={"range": "7d"}, headers=auth_headers, timeout=30)
        assert r.status_code == 200, r.text
        d = r.json()
        text = str(d).lower()
        assert "speedup" in text or "mv" in text or "materialized" in text or "full" in text


# ---------- Ops / Lab ----------
class TestOpsLab:
    def test_system_stats(self, auth_headers):
        r = requests.get(f"{BASE_URL}/api/system/stats", headers=auth_headers, timeout=15)
        # accept both authed and non-authed variants
        if r.status_code == 401:
            r = requests.get(f"{BASE_URL}/api/system/stats", timeout=15)
        assert r.status_code == 200, r.text

    def test_decode_id(self):
        r = requests.get(f"{BASE_URL}/api/lab/snowflake",
                         params={"id": "216656406189707264"}, timeout=15)
        assert r.status_code == 200, r.text

    def test_id_burst(self):
        r = requests.get(f"{BASE_URL}/api/lab/snowflake/burst", params={"count": 1000}, timeout=30)
        assert r.status_code == 200, r.text
        d = r.json()
        text = str(d).lower()
        assert "collision" in text or "ids" in text or "count" in text

    def test_ring(self):
        r = requests.get(f"{BASE_URL}/api/lab/ring",
                         params={"shards_before": 4, "shards_after": 8, "vnodes": 128, "keys": 5000},
                         timeout=15)
        assert r.status_code == 200, r.text

    def test_bloom_add_and_probe(self):
        key = f"bloomkey-{uuid.uuid4().hex[:6]}"
        r1 = requests.post(f"{BASE_URL}/api/lab/bloom/add", json={"key": key}, timeout=15)
        assert r1.status_code == 200, r1.text
        r2 = requests.get(f"{BASE_URL}/api/lab/bloom/test", params={"key": key}, timeout=15)
        assert r2.status_code == 200, r2.text
        d = r2.json()
        assert d.get("maybe") is True
