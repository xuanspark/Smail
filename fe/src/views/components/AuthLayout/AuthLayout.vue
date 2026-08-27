<template>
  <div class="auth-layout">
    <div class="auth-layout__card">
      <aside class="auth-layout__intro">
        <div class="auth-layout__brand">
          <BrandLogo
            variant="login"
            surface
            class="auth-layout__logo"
            :alt="brandName"
          />
          <div class="auth-layout__brand-copy">
            <div class="auth-layout__brand-name">{{ brandName }}</div>
            <div v-if="brandCaption" class="auth-layout__brand-caption">
              {{ brandCaption }}
            </div>
          </div>
        </div>

        <div v-if="introTitle || introDescription" class="auth-layout__intro-copy">
          <h1 v-if="introTitle" class="auth-layout__intro-title">
            {{ introTitle }}
          </h1>
          <p v-if="introDescription" class="auth-layout__intro-description">
            {{ introDescription }}
          </p>
        </div>

        <slot name="intro-extra" />
      </aside>

      <main class="auth-layout__content">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup>
import BrandLogo from '../BrandLogo/BrandLogo.vue'
import { authLayoutProps } from './AuthLayout.js'

defineProps(authLayoutProps)
</script>

<style scoped src="./AuthLayout.css"></style>
