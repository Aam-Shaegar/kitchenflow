"""Process-level KitchenFlow scenarios. Run via scripts/e2e.sh."""
import json
import os
import socket
import subprocess
import sys
import time
from contextlib import contextmanager
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

RID = "550e8400-e29b-41d4-a716-446655440000"
ITEM = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb1"


def equal(actual, expected, label):
    if actual != expected:
        raise AssertionError(f"{label}: expected {expected!r}, got {actual!r}")


def request(url, method="GET", body=None, token=None, code=200, key=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    if key:
        headers["Idempotency-Key"] = key
    data = None if body is None else json.dumps(body).encode()
    try:
        response = urlopen(Request(url, data, headers, method=method), timeout=5)
    except HTTPError as err:
        response = err
    with response:
        raw = response.read()
        equal(response.status, code, f"{method} {url} ({raw[:300]!r})")
        return json.loads(raw) if raw else None


@contextmanager
def service(binary, env, health):
    with subprocess.Popen([binary], env=env) as process:
        try:
            deadline = time.monotonic() + 20
            while True:
                if process.poll() is not None:
                    raise RuntimeError(f"{binary.name} exited with {process.returncode}")
                try:
                    request(health)
                    break
                except (URLError, AssertionError):
                    if time.monotonic() >= deadline:
                        raise
                    time.sleep(0.1)
            yield
        finally:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                raise


def run(folder):
    registry = folder / "auth.json"
    keys = folder / "callback.json"

    def issue(role):
        return subprocess.check_output([sys.executable, "scripts/issue-token.py", "--file", registry,
                                        "--subject", "e2e-" + role, "--role", role], text=True).strip()

    admin, user = issue("admin"), issue("user")
    subprocess.run([sys.executable, "scripts/issue-callback-key.py", "--file", keys,
                    "--restaurant", RID, "--key-id", "e2e"], check=True, stdout=subprocess.DEVNULL)
    with socket.socket() as first, socket.socket() as second:
        for sock in (first, second):
            sock.bind(("127.0.0.1", 0))
        api_port, mock_port = first.getsockname()[1], second.getsockname()[1]
    host = f"127.0.0.1:{api_port}"
    api = f"http://{host}/api/v1"
    mock = f"http://127.0.0.1:{mock_port}"
    env = dict(os.environ, SERVER_ADDR=host, PORT=str(mock_port), AUTH_CREDENTIALS_FILE=str(registry),
               CALLBACK_KEYS_FILE=str(keys), CALLBACK_KEY_ID="e2e", RESTAURANT_ID=RID, MOCK_MODE="manual",
               LOGGER_FOLDER=str(folder / "logs"), MIGRATIONS_DIR="migrations", KITCHEN_CALLBACK_URL=f"http://{host}",
               RESTAURANT_ALLOWED_ORIGINS=mock, RESTAURANT_ALLOWED_PRIVATE_CIDRS="127.0.0.0/8")

    def checkout(address, expected):
        cart = request(api + "/cart/items", "POST", {"restaurant_id": RID, "menu_item_id": ITEM,
                       "quantity": 2}, user, 201)
        equal((cart["items"][0]["unit_price"], cart["total_amount"]), (54900, 109800), "cart price")
        request(api + "/cart/items", "POST", {"restaurant_id": other, "menu_item_id": dish,
                                             "quantity": 1}, user, 409)
        body = {"delivery_address": address}
        order = request(api + "/orders", "POST", body, user, 201, address)
        equal(order["status"], expected, address)
        replay = request(api + "/orders", "POST", body, user, 201, address)
        equal(replay["id"], order["id"], "idempotent checkout")
        return order["id"]

    def verify(order, status):
        equal(request(api + "/orders/" + order, token=user)["status"], status, "customer order")
        equal(request(mock + "/api/v1/restaurant/orders/" + order)["status"], status, "restaurant order")

    def deliver(order, prefix):
        path = "/orders/" + order
        steps = ["preparing", "ready", "delivering", "delivered"]
        for status in steps:
            request(mock + "/api/v1/restaurant" + path + "/status", "POST", {"status": status}, code=204)
        deadline = time.monotonic() + 15
        while True:
            history = request(api + path + "/history", token=user)
            if history[-1]["status"] == "delivered" or time.monotonic() >= deadline:
                break
            time.sleep(0.1)
        equal([e["status"] for e in history], prefix + steps, "customer status history")
        for event in history[-len(steps):]:
            equal((event["source"], bool(event.get("event_id"))), ("restaurant", True), "callback evidence")
        verify(order, "delivered")

    with service(folder / "api", env, api + "/health/ready"):
        equal(RID in [r["id"] for r in request(api + "/restaurants")], True, "seed restaurant")
        menu = request(api + f"/restaurants/{RID}/menu")
        equal([i["name"] for c in menu["categories"] for i in c["items"] if i["id"] == ITEM],
              ["Classic Burger"], "seed menu")
        request(api + f"/restaurants/{RID}", "PATCH", {"integration_base_url": mock}, admin)
        other = request(api + "/restaurants", "POST", {"name": "Other"}, admin, 201)["id"]
        menu_url = api + f"/restaurants/{other}/menu"
        cat = request(menu_url + "/categories", "POST", {"name": "Sides"}, admin, 201)["id"]
        dish = request(menu_url + "/items", "POST", {"category_id": cat, "name": "Side", "price": 100},
                       admin, 201)["id"]

        # The configured restaurant is offline until its process starts.
        failed = checkout("Outage", "integration_failed")
        with service(folder / "mock", env, mock + "/health"):
            retried = request(api + "/orders/" + failed + "/retry", "POST", token=user)
            equal((retried["id"], retried["status"]), (failed, "confirmed"), "retry")
            deliver(failed, ["pending", "integration_failed", "pending", "confirmed"])
            happy = checkout("Delivery", "confirmed")
            deliver(happy, ["pending", "confirmed"])
            verify(checkout("Reject this order", "cancelled"), "cancelled")
            cancel = checkout("Customer cancellation", "confirmed")
            request(api + "/orders/" + cancel + "/cancel", "POST", token=user)
            verify(cancel, "cancelled")
    print("PASS: checkout, idempotency, delivery callbacks, rejection, retry and cancellation")


if __name__ == "__main__":
    run(Path(sys.argv[1]))
