// Sends logins faster than Identity can hash their passwords, at a fixed rate however
// slowly it answers, as real users would. It measures how many logins still succeed per
// second, how long they take, and how many are turned away.
//
// Each VU logs in a user of its own, one login at a time. Identity counts attempts per
// email and address before hashing, and every k6 request comes from one address on kind,
// so one shared user would mostly get 429s that never reach the hashing queue.
//
// The default rate is a few times what Identity gets through on a laptop, about 80
// logins a second, since four hashes at once take about 50 ms each. VUS must cover the
// rate times the Edge's 2s timeout, or k6 drops iterations and the load falls short;
// setup registers that many users.
//
// On kind, the Edge's limits stop this at once, so raise them before a run:
//
//   kubectl -n ride-sharing set env deploy/edge RATE_LIMIT_IP_RATE=100000 RATE_LIMIT_IP_BURST=100000 RATE_LIMIT_LOGIN_RATE=100000 RATE_LIMIT_LOGIN_BURST=100000 RATE_LIMIT_REGISTER_RATE=100000 RATE_LIMIT_REGISTER_BURST=100000
//   k6 run test/k6/login-burst.js
//
// Afterwards, remove them with the same names each followed by "-". A Tilt deploy keeps
// them: it leaves alone the env variables the manifest doesn't name.
//
// The run fails its threshold if any login gets a 500.
import http from "k6/http";
import exec from "k6/execution";

const baseURL = __ENV.BASE_URL || "https://ride-sharing.localhost";
const params = { headers: { "Content-Type": "application/json" } };
const vus = Number(__ENV.VUS || 700);

export const options = {
  scenarios: {
    logins: {
      executor: "constant-arrival-rate",
      rate: Number(__ENV.RATE || 300),
      timeUnit: "1s",
      duration: __ENV.DURATION || "60s",
      preAllocatedVUs: vus,
      maxVUs: vus,
    },
  },
  setupTimeout: "5m",
  // The empty ones only make the summary show each status apart.
  thresholds: {
    "http_reqs{scenario:logins,status:500}": ["count==0"],
    "http_reqs{scenario:logins,status:200}": [],
    "http_reqs{scenario:logins,status:429}": [],
    "http_reqs{scenario:logins,status:503}": [],
    "http_req_duration{scenario:logins,status:200}": [],
    "http_req_duration{scenario:logins,status:503}": [],
  },
  summaryTrendStats: ["min", "med", "p(95)", "p(99)", "max"],
};

// New users every run: a run that overloads Identity can leave its users locked out.
// Registered four at a time, as many as Identity hashes at once.
export function setup() {
  const prefix = `k6-${Date.now()}`;
  const users = [];
  for (let i = 0; i < vus; i++) {
    users.push({ email: `${prefix}-${i}@example.pt`, password: "k6 login burst password" });
  }
  for (let i = 0; i < users.length; i += 4) {
    const requests = users.slice(i, i + 4).map((user) => [
      "POST",
      `${baseURL}/v1/auth/register`,
      JSON.stringify({ ...user, role: "rider" }),
      params,
    ]);
    for (const res of http.batch(requests)) {
      if (res.status !== 201) {
        exec.test.abort(`register: ${res.status} ${res.body}`);
      }
    }
  }
  return users;
}

export default function (users) {
  const user = users[exec.vu.idInTest - 1];
  http.post(`${baseURL}/v1/auth/login`, JSON.stringify(user), params);
}
