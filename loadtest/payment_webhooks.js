import http from 'k6/http';
import exec from 'k6/execution';
import crypto from 'k6/crypto';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

const webhookPath = '/webhooks/payment';
const healthzPath = '/healthz';
const selectedScenario = (__ENV.SCENARIO || 'normal').trim();
const baseURL = (__ENV.K6_WEBHOOK_BASE_URL || 'http://localhost:8080').trim();
const signingSecret = __ENV.WEBHOOK_SIGNING_SECRET || 'dev-webhook-signing-secret';

const processedResponses = new Counter('processed_responses');
const duplicateResponses = new Counter('duplicate_responses');
const unauthorizedResponses = new Counter('unauthorized_responses');
const unexpectedResponses = new Counter('unexpected_responses');

const scenarioProfiles = {
  normal: {
    executor: 'constant-arrival-rate',
    exec: 'runNormalScenario',
    duration: '30s',
    timeUnit: '1s',
    rate: integerEnv('K6_NORMAL_RATE', 5),
    preAllocatedVUs: 10,
    maxVUs: 30,
  },
  duplicate: {
    executor: 'per-vu-iterations',
    exec: 'runDuplicateScenario',
    vus: 1,
    iterations: 12,
    maxDuration: '30s',
  },
  mixed_signatures: {
    executor: 'constant-arrival-rate',
    exec: 'runMixedSignatureScenario',
    duration: '30s',
    timeUnit: '1s',
    rate: 6,
    preAllocatedVUs: 10,
    maxVUs: 30,
  },
  spike: {
    executor: 'constant-arrival-rate',
    exec: 'runSpikeScenario',
    duration: '30s',
    timeUnit: '1s',
    rate: integerEnv('K6_SPIKE_RATE', 2000),
    preAllocatedVUs: 25,
    maxVUs: 120,
  },
};

if (!scenarioProfiles[selectedScenario]) {
  throw new Error(`unsupported SCENARIO ${selectedScenario}`);
}

export const options = {
  scenarios: {
    [selectedScenario]: {
      ...scenarioProfiles[selectedScenario],
      tags: { loadtest_scenario: selectedScenario },
    },
  },
  thresholds: thresholdsFor(selectedScenario),
};

export function setup() {
  const response = http.get(`${baseURL}${healthzPath}`, {
    tags: { endpoint: healthzPath },
  });

  if (response.status !== 200) {
    exec.test.abort(`health check failed with status ${response.status}`);
  }

  return { runID: new Date().toISOString().replace(/[^0-9]/g, '') };
}

export function runNormalScenario(data) {
  const ids = uniqueIDs(data.runID, 'normal');
  const response = sendWebhookRequest({
    providerEventID: ids.providerEventID,
    paymentID: ids.paymentID,
    eventType: eventTypeForIteration(),
    validSignature: true,
  });

  const body = response.json();
  const status = body && body.status;

  const passed = check(response, {
    'normal returns 200': (res) => res.status === 200,
    'normal returns processed': () => status === 'processed',
  });

  recordOutcome(response, status, passed);
}

export function runDuplicateScenario(data) {
  const response = sendWebhookRequest({
    providerEventID: `evt_duplicate_shared_${data.runID}`,
    paymentID: `pay_duplicate_shared_${data.runID}`,
    eventType: 'payment.failed',
    validSignature: true,
  });

  const body = response.json();
  const status = body && body.status;

  const passed = check(response, {
    'duplicate returns 200': (res) => res.status === 200,
    'duplicate returns processed or duplicate': () => status === 'processed' || status === 'duplicate',
  });

  recordOutcome(response, status, passed);
}

export function runMixedSignatureScenario(data) {
  const ids = uniqueIDs(data.runID, 'mixed');
  const validSignature = exec.scenario.iterationInTest % 2 === 0;
  const response = sendWebhookRequest({
    providerEventID: ids.providerEventID,
    paymentID: ids.paymentID,
    eventType: eventTypeForIteration(),
    validSignature,
  });

  const body = response.json();
  const status = body && body.status;

  const passed = check(response, {
    'mixed valid signatures return 200': (res) => (validSignature ? res.status === 200 : true),
    'mixed invalid signatures return 401': (res) => (!validSignature ? res.status === 401 : true),
    'mixed valid signatures are processed': () => (validSignature ? status === 'processed' : true),
    'mixed invalid signatures are unauthorized': () => (!validSignature ? status === 'unauthorized' : true),
  });

  recordOutcome(response, status, passed);
}

export function runSpikeScenario(data) {
  const ids = uniqueIDs(data.runID, 'spike');
  const response = sendWebhookRequest({
    providerEventID: ids.providerEventID,
    paymentID: ids.paymentID,
    eventType: eventTypeForIteration(),
    validSignature: true,
  });

  const body = response.json();
  const status = body && body.status;

  const passed = check(response, {
    'spike returns 200 or 500': (res) => res.status === 200 || res.status === 500,
    'spike returns processed or internal_error': () => status === 'processed' || status === 'internal_error',
  });

  recordOutcome(response, status, passed);
}

function thresholdsFor(scenarioName) {
  const common = {
    checks: ['rate>0.99'],
    http_req_duration: ['p(95)<1500'],
    unexpected_responses: ['count==0'],
  };

  switch (scenarioName) {
    case 'normal':
      return {
        ...common,
        http_req_failed: ['rate<0.05'],
        processed_responses: ['count>0'],
      };
    case 'duplicate':
      return {
        ...common,
        http_req_failed: ['rate<0.01'],
        duplicate_responses: ['count>0'],
      };
    case 'mixed_signatures':
      return {
        ...common,
        http_req_failed: ['rate<0.6'],
        processed_responses: ['count>0'],
        unauthorized_responses: ['count>0'],
      };
    case 'spike':
      return {
        checks: ['rate>0.95'],
        http_req_duration: ['p(95)<5000'],
        http_req_failed: ['rate<0.35'],
        processed_responses: ['count>0'],
        unexpected_responses: ['count==0'],
      };
    default:
      return common;
  }
}

function sendWebhookRequest({ providerEventID, paymentID, eventType, validSignature }) {
  const payload = JSON.stringify({
    provider_event_id: providerEventID,
    payment_id: paymentID,
    event_type: eventType,
    event_timestamp: new Date().toISOString(),
  });

  const signature = validSignature ? signPayload(payload) : 'invalid-signature';

  return http.post(`${baseURL}${webhookPath}`, payload, {
    headers: {
      'Content-Type': 'application/json',
      'X-Webhook-Signature': signature,
    },
    tags: {
      endpoint: webhookPath,
      loadtest_scenario: selectedScenario,
      signature: validSignature ? 'valid' : 'invalid',
    },
  });
}

function signPayload(payload) {
  return crypto.hmac('sha256', signingSecret, payload, 'hex');
}

function uniqueIDs(runID, prefix) {
  const scenarioIteration = exec.scenario.iterationInTest;
  const vuID = exec.vu.idInTest;
  const suffix = `${runID}_${vuID}_${scenarioIteration}`;

  return {
    providerEventID: `evt_${prefix}_${suffix}`,
    paymentID: `pay_${prefix}_${suffix}`,
  };
}

function eventTypeForIteration() {
  const eventTypes = ['payment.pending', 'payment.paid', 'payment.failed', 'payment.expired'];
  return eventTypes[exec.scenario.iterationInTest % eventTypes.length];
}

function recordOutcome(response, status, passed) {
  if (response.status === 200 && status === 'processed') {
    processedResponses.add(1);
    return;
  }

  if (response.status === 200 && status === 'duplicate') {
    duplicateResponses.add(1);
    return;
  }

  if (response.status === 401 && status === 'unauthorized') {
    unauthorizedResponses.add(1);
    return;
  }

  if (!passed) {
    unexpectedResponses.add(1);
    return;
  }

  if (response.status >= 500) {
    return;
  }

  unexpectedResponses.add(1);
}

function integerEnv(name, fallback) {
  const raw = __ENV[name];
  if (!raw) {
    return fallback;
  }

  const value = Number.parseInt(raw, 10);
  if (Number.isNaN(value) || value <= 0) {
    throw new Error(`invalid ${name}: ${raw}`);
  }

  return value;
}
