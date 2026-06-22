import http from 'k6/http';
import exec from 'k6/execution';
import crypto from 'k6/crypto';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

const webhookPath = '/webhooks/payment';
const healthzPath = '/healthz';
const providerTimeoutMs = 5000;
const selectedScenario = (__ENV.SCENARIO || 'normal').trim();
const baseURL = (__ENV.K6_WEBHOOK_BASE_URL || 'http://localhost:8080').trim();
const signingSecret = __ENV.WEBHOOK_SIGNING_SECRET || 'dev-webhook-signing-secret';

const processedResponses = new Counter('processed_responses');
const duplicateResponses = new Counter('duplicate_responses');
const unauthorizedResponses = new Counter('unauthorized_responses');
const unexpectedResponses = new Counter('unexpected_responses');
const supportedScenarioNames = ['normal', 'duplicate', 'mixed_signatures', 'spike', 'capacity_ramp', 'noisy_neighbor', 'hot_payments', 'out_of_order_race'];

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
    preAllocatedVUs: 80,
    maxVUs: 240,
  },
  capacity_ramp: {
    executor: 'ramping-arrival-rate',
    exec: 'runCapacityRampScenario',
    startRate: integerEnv('K6_CAPACITY_START_RATE', 10),
    timeUnit: '1s',
    preAllocatedVUs: 50,
    maxVUs: 250,
    stages: capacityRampStages(integerEnv('K6_CAPACITY_PEAK_RATE', 250)),
  },
  hot_payments: {
    executor: 'constant-arrival-rate',
    exec: 'runHotPaymentsScenario',
    duration: '45s',
    timeUnit: '1s',
    rate: integerEnv('K6_HOT_PAYMENT_RATE', 150),
    preAllocatedVUs: 60,
    maxVUs: 240,
  },
  out_of_order_race: {
    executor: 'constant-arrival-rate',
    exec: 'runOutOfOrderRaceScenario',
    duration: '45s',
    timeUnit: '1s',
    rate: integerEnv('K6_OUT_OF_ORDER_RATE', 300),
    preAllocatedVUs: 90,
    maxVUs: 320,
  },
};

if (!supportedScenarioNames.includes(selectedScenario)) {
  throw new Error(`unsupported SCENARIO ${selectedScenario}`);
}

export const options = {
  scenarios: scenariosFor(selectedScenario),
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
    trafficStream: 'primary',
  });

  const body = response.json();
  const status = body && body.status;

  const passed = check(response, {
    'spike returns 200 or 500': (res) => res.status === 200 || res.status === 500,
    'spike returns processed or internal_error': () => status === 'processed' || status === 'internal_error',
  });

  recordOutcome(response, status, passed);
}

export function runCapacityRampScenario(data) {
  const ids = uniqueIDs(data.runID, 'capacity');
  const response = sendWebhookRequest({
    providerEventID: ids.providerEventID,
    paymentID: ids.paymentID,
    eventType: eventTypeForIteration(),
    validSignature: true,
    trafficStream: 'primary',
  });

  const body = response.json();
  const status = body && body.status;

  const passed = check(response, {
    'capacity_ramp returns 200 or 500': (res) => res.status === 200 || res.status === 500,
    'capacity_ramp returns processed or internal_error': () => status === 'processed' || status === 'internal_error',
  });

  recordOutcome(response, status, passed);
}

export function runNoisyNeighborBaselineScenario(data) {
  const ids = uniqueIDs(data.runID, 'baseline');
  const response = sendWebhookRequest({
    providerEventID: ids.providerEventID,
    paymentID: ids.paymentID,
    eventType: eventTypeForIteration(),
    validSignature: true,
    trafficStream: 'baseline',
  });

  const body = response.json();
  const status = body && body.status;

  const passed = check(response, {
    'noisy baseline returns 200': (res) => res.status === 200,
    'noisy baseline returns processed': () => status === 'processed',
  });

  recordOutcome(response, status, passed);
}

export function runNoisyNeighborSpikeScenario(data) {
  const ids = uniqueIDs(data.runID, 'noisy');
  const response = sendWebhookRequest({
    providerEventID: ids.providerEventID,
    paymentID: ids.paymentID,
    eventType: eventTypeForIteration(),
    validSignature: true,
    trafficStream: 'noisy',
  });

  const body = response.json();
  const status = body && body.status;

  const passed = check(response, {
    'noisy spike returns 200 or 500': (res) => res.status === 200 || res.status === 500,
    'noisy spike returns processed or internal_error': () => status === 'processed' || status === 'internal_error',
  });

  recordOutcome(response, status, passed);
}

export function runHotPaymentsScenario(data) {
  const ids = uniqueIDs(data.runID, 'hot');
  const response = sendWebhookRequest({
    providerEventID: ids.providerEventID,
    paymentID: hotPaymentID(data.runID),
    eventType: eventTypeForIteration(),
    validSignature: true,
    trafficStream: 'primary',
  });

  const body = response.json();
  const status = body && body.status;

  const passed = check(response, {
    'hot_payments returns 200 or 500': (res) => res.status === 200 || res.status === 500,
    'hot_payments returns processed, internal_error, or duplicate': () =>
      status === 'processed' || status === 'internal_error' || status === 'duplicate',
  });

  recordOutcome(response, status, passed);
}

export function runOutOfOrderRaceScenario(data) {
  const raceEvent = outOfOrderEventForIteration(data.runID);
  const response = sendWebhookRequest({
    providerEventID: raceEvent.providerEventID,
    paymentID: raceEvent.paymentID,
    eventType: raceEvent.eventType,
    validSignature: true,
    trafficStream: 'race',
    eventTimestamp: raceEvent.eventTimestamp,
  });

  const body = response.json();
  const status = body && body.status;

  const passed = check(response, {
    'out_of_order_race returns 200 or 500': (res) => res.status === 200 || res.status === 500,
    'out_of_order_race returns processed or internal_error': () => status === 'processed' || status === 'internal_error',
  });

  recordOutcome(response, status, passed);
}

function thresholdsFor(scenarioName) {
  const common = {
    checks: ['rate>0.99'],
    http_req_duration: [`p(95)<${providerTimeoutMs}`],
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
        http_req_duration: [`p(95)<${providerTimeoutMs}`],
        http_req_failed: ['rate<0.35'],
        processed_responses: ['count>0'],
        unexpected_responses: ['count==0'],
      };
    case 'capacity_ramp':
      return {
        checks: ['rate>0.95'],
        http_req_duration: [`p(95)<${providerTimeoutMs}`],
        http_req_failed: ['rate<0.35'],
        processed_responses: ['count>0'],
        unexpected_responses: ['count==0'],
      };
    case 'noisy_neighbor':
      return {
        checks: ['rate>0.95'],
        'http_req_duration{traffic_stream:baseline}': [`p(95)<${providerTimeoutMs}`],
        http_req_failed: ['rate<0.35'],
        processed_responses: ['count>0'],
        unexpected_responses: ['count==0'],
      };
    case 'hot_payments':
      return {
        checks: ['rate>0.95'],
        http_req_duration: [`p(95)<${providerTimeoutMs}`],
        http_req_failed: ['rate<0.35'],
        processed_responses: ['count>0'],
        unexpected_responses: ['count==0'],
      };
    case 'out_of_order_race':
      return {
        checks: ['rate>0.95'],
        http_req_duration: [`p(95)<${providerTimeoutMs}`],
        http_req_failed: ['rate<0.35'],
        processed_responses: ['count>0'],
        unexpected_responses: ['count==0'],
      };
    default:
      return common;
  }
}

function scenariosFor(scenarioName) {
  if (scenarioName === 'noisy_neighbor') {
    return {
      noisy_neighbor_baseline: {
        executor: 'constant-arrival-rate',
        exec: 'runNoisyNeighborBaselineScenario',
        duration: '45s',
        timeUnit: '1s',
        rate: integerEnv('K6_NOISY_BASELINE_RATE', 5),
        preAllocatedVUs: 10,
        maxVUs: 40,
        tags: { loadtest_scenario: scenarioName, traffic_stream: 'baseline' },
      },
      noisy_neighbor_spike: {
        executor: 'ramping-arrival-rate',
        exec: 'runNoisyNeighborSpikeScenario',
        startRate: integerEnv('K6_NOISY_SPIKE_START_RATE', 20),
        timeUnit: '1s',
        preAllocatedVUs: 30,
        maxVUs: 180,
        stages: [
          { target: integerEnv('K6_NOISY_SPIKE_START_RATE', 20), duration: '10s' },
          { target: integerEnv('K6_NOISY_SPIKE_PEAK_RATE', 200), duration: '15s' },
          { target: integerEnv('K6_NOISY_SPIKE_PEAK_RATE', 200), duration: '10s' },
          { target: integerEnv('K6_NOISY_SPIKE_START_RATE', 20), duration: '10s' },
        ],
        tags: { loadtest_scenario: scenarioName, traffic_stream: 'noisy' },
      },
    };
  }

  return {
    [scenarioName]: {
      ...scenarioProfiles[scenarioName],
      tags: { loadtest_scenario: scenarioName },
    },
  };
}

function sendWebhookRequest({ providerEventID, paymentID, eventType, validSignature, trafficStream = 'primary', eventTimestamp = new Date().toISOString() }) {
  const payload = JSON.stringify({
    provider_event_id: providerEventID,
    payment_id: paymentID,
    event_type: eventType,
    event_timestamp: eventTimestamp,
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
      traffic_stream: trafficStream,
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

function hotPaymentID(runID) {
  const setSize = integerEnv('K6_HOT_PAYMENT_SET_SIZE', 5);
  return `pay_hot_shared_${runID}_${exec.scenario.iterationInTest % setSize}`;
}

function outOfOrderEventForIteration(runID) {
  const setSize = integerEnv('K6_OUT_OF_ORDER_PAYMENT_SET_SIZE', 20);
  const paymentIndex = exec.scenario.iterationInTest % setSize;
  const sequenceIndex = Math.floor(exec.scenario.iterationInTest / setSize) % outOfOrderTemplates.length;
  const template = outOfOrderTemplates[sequenceIndex];
  const paymentID = `pay_race_shared_${runID}_${paymentIndex}`;
  const providerEventID = `evt_race_${template.eventKey}_${runID}_${paymentIndex}_${exec.scenario.iterationInTest}`;
  const baseTimestampMs = Date.parse('2026-06-19T00:00:00.000Z') + paymentIndex * 1000;

  return {
    providerEventID,
    paymentID,
    eventType: template.eventType,
    eventTimestamp: new Date(baseTimestampMs + template.offsetSeconds * 1000).toISOString(),
  };
}

const outOfOrderTemplates = [
  { eventKey: 'paid_latest', eventType: 'payment.paid', offsetSeconds: 180 },
  { eventKey: 'pending_original', eventType: 'payment.pending', offsetSeconds: 0 },
  { eventKey: 'failed_mid', eventType: 'payment.failed', offsetSeconds: 120 },
  { eventKey: 'pending_after_paid', eventType: 'payment.pending', offsetSeconds: 240 },
  { eventKey: 'pending_late_old', eventType: 'payment.pending', offsetSeconds: 30 },
];

function capacityRampStages(peakRate) {
  const quarter = Math.max(1, Math.floor(peakRate / 4));
  const half = Math.max(1, Math.floor(peakRate / 2));
  const threeQuarter = Math.max(1, Math.floor((peakRate * 3) / 4));

  return [
    { target: quarter, duration: '15s' },
    { target: half, duration: '15s' },
    { target: threeQuarter, duration: '15s' },
    { target: peakRate, duration: '15s' },
  ];
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
