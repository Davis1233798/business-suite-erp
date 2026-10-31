// This system has no infrastructure outbox consumer.
export async function dispatchInfrastructureOutbox() {
  return { configured: false, sent: 0, mesSynced: 0 };
}
