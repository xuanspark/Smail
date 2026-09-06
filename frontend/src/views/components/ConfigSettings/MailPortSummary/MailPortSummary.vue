<template>
  <section class="mail-port-summary" aria-label="邮件协议端口汇总">
    <p class="summary-note">端口自动读取 SMTP、IMAP、POP3 配置，不能在邮箱策略中修改。这里显示已应用的配置值，不是端口在线检测；如有待重启提示，请重启后端后再连接。</p>
    <div class="summary-grid">
      <article v-for="service in services" :key="service.name" class="protocol-card">
        <strong>{{ service.name }}</strong>
        <dl>
          <div v-for="port in service.ports" :key="port.label" class="port-row">
            <dt>{{ port.label }}</dt>
            <dd>{{ port.value ?? '未读取' }}</dd>
          </div>
        </dl>
      </article>
    </div>
  </section>
</template>

<script setup>
defineProps({ services: { type: Array, required: true } })
</script>

<style scoped src="./MailPortSummary.css"></style>
