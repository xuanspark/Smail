<script setup>
import { useStartupStatus } from "./StartupStatus.js"

const {
  buttonLabel,
  description,
  isLoading,
  retry,
  title
} = useStartupStatus()
</script>

<template>
  <main class="startup-status" aria-live="polite">
    <section class="startup-status__card">
      <span class="startup-status__label">Smail</span>
      <h1>{{ title }}</h1>
      <p>{{ description }}</p>
      <el-button
        v-if="!isLoading"
        type="primary"
        size="large"
        @click="retry"
      >
        {{ buttonLabel }}
      </el-button>
      <div v-else class="startup-status__loading" role="status">
        <span class="startup-status__spinner" aria-hidden="true"></span>
        <span>请稍候</span>
      </div>
    </section>
  </main>
</template>

<style scoped src="./StartupStatus.css"></style>
