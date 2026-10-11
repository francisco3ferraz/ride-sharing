// Fills Identity's four argon2id hashing slots and keeps a queue waiting behind them,
// to measure Identity's peak memory and how long a queued login waits.
//
//   k6 run -e VUS=200 test/k6/login-burst.js
import http from "k6/http";
import { check } from "k6";
import exec from "k6/execution";

const baseURL = __ENV.BASE_URL || "https://ride-sharing.localhost";
const params = { headers: { "Content-Type": "application/json" } };

export const options = {
  scenarios: {
    logins: {
      executor: "constant-vus",
      vus: Number(__ENV.VUS || 200),
      duration: __ENV.DURATION || "90s",
    },
  },
  // No conditions: this only makes the summary show the logins without setup's register.
  thresholds: { "http_req_duration{scenario:logins}": [] },
  summaryTrendStats: ["min", "med", "p(95)", "p(99)", "max"],
};

// A new user every run, so reruns don't collide on the email.
export function setup() {
  const user = { email: `k6-${Date.now()}@example.pt`, password: "k6 login burst password" };
  const res = http.post(`${baseURL}/v1/auth/register`, JSON.stringify({ ...user, role: "rider" }), params);
  if (res.status !== 201) {
    exec.test.abort(`register: ${res.status} ${res.body}`);
  }
  return user;
}

export default function (user) {
  const res = http.post(`${baseURL}/v1/auth/login`, JSON.stringify(user), params);
  check(res, { "status is 200": (r) => r.status === 200 });
}
