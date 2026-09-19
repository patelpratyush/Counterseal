package dev.handoffguard.workflow;

public enum Stage {
    SUPPORT("support-agent", "orders.get", "orders.read", "order_id"),
    BILLING("billing-agent", "refund.create", "refunds.create", "refund_id"),
    NOTIFICATION("notification-agent", "email.send", "email.send", "notification_id");

    final String agent, tool, action, receiptKey;
    Stage(String agent, String tool, String action, String receiptKey) {
        this.agent = agent; this.tool = tool; this.action = action; this.receiptKey = receiptKey;
    }
}
