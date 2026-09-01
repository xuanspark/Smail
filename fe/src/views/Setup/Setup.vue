<template>
<div id="main" :class="['setup-page', {
  'database-page': active === 1,
  'dns-page': active === 4,
  'ssl-page': active === 5
}]">
    <el-steps :active="active" align-center finish-status="success" id="status">
      <el-step :title="lang.welcome"/>
      <el-step :title="lang.setDatabase"/>
      <el-step :title="lang.setAdminPassword"/>
      <el-step :title="lang.SetDomail"/>
      <el-step :title="lang.setDNS"/>
      <el-step :title="lang.setSSL"/>
    </el-steps>


    <div v-if="active === 0" class="ctn">
      <div class="desc">
        <h2>{{ lang.tks_smail }}</h2>
        <div style="margin-top: 10px;">{{ lang.guid_desc }}</div>
      </div>
    </div>

    <div v-if="active === 1" class="ctn setup-form-panel">
      <div class="desc">
        <h2>{{ lang.select_db }}</h2>
        <div style="margin-top: 10px;">{{ lang.db_desc }}</div>
      </div>
      <div class="form database-form">
        <el-form label-position="top">
          <el-form-item :label="lang.type">
            <el-select :placeholder="lang.db_select_ph" v-model="dbSettings.type"
                       :disabled="databaseLoading" @change="handleDbTypeChange">
              <el-option label="MySQL" value="mysql"/>
              <el-option label="SQLite3" value="sqlite"/>
              <el-option label="PostgreSQL" value="postgres"/>
            </el-select>
          </el-form-item>

          <el-alert
            v-if="databaseError"
            :title="databaseError"
            type="error"
            show-icon
            :closable="false"
            class="database-error"
          />

          <div v-if="dbSettings.type === 'mysql' || dbSettings.type === 'postgres'" class="mysql-fields">
            <div class="database-hint">
              {{ dbSettings.type === 'mysql' ? lang.mysql_create_desc : lang.postgres_create_desc }}
            </div>


            <el-form-item class="database-user-field" :label="lang.db_user">
              <el-input v-model="dbSettings.user" @input="clearDatabaseError"
                        :disabled="databaseLoading"
                        :placeholder="dbSettings.type === 'mysql' ? lang.db_user_ph : lang.pg_user_ph"
                        autocomplete="username"></el-input>
            </el-form-item>

            <el-form-item class="database-host-field" :label="lang.db_host">
              <el-input v-model="dbSettings.host" @input="clearDatabaseError"
                        :disabled="databaseLoading"
                        :placeholder="lang.db_host_ph"></el-input>
            </el-form-item>

            <el-form-item class="database-port-field" :label="lang.db_port">
              <el-input v-model="dbSettings.port" @input="clearDatabaseError"
                        :disabled="databaseLoading"
                        type="number"
                        min="1"
                        max="65535"
                        step="1"
                        inputmode="numeric"
                        :placeholder="lang.db_port_ph"></el-input>
            </el-form-item>

            <el-form-item class="database-name-field" :label="lang.db_name">
              <el-input v-model="dbSettings.name" @input="clearDatabaseError"
                        :disabled="databaseLoading"
                        :placeholder="lang.db_name_ph"></el-input>
            </el-form-item>

            <el-form-item class="database-password-field" :label="lang.db_password">
              <el-input v-model="dbSettings.password" @input="clearDatabaseError"
                        :disabled="databaseLoading"
                        type="password"
                        :placeholder="dbSettings.type === 'mysql' ? lang.db_password_ph : lang.pg_password_ph" autocomplete="new-password"
                        show-password></el-input>
              <div class="database-field-tip">{{ lang.db_password_desc }}</div>
            </el-form-item>

          </div>

          <div class="database-retry-tip">{{ lang.db_init_retry_tip }}</div>

          <el-form-item :label="lang.sqlite_db_path" v-if="dbSettings.type === 'sqlite'">
            <el-input v-model="dbSettings.dsn" @input="clearDatabaseError"
                      :disabled="databaseLoading"
                      placeholder="请输入 SQLite 数据文件路径"></el-input>
          </el-form-item>
        </el-form>
      </div>
    </div>


    <div v-if="active === 2" class="ctn">
      <div class="desc">
        <h2>{{ lang.setAdminPassword }}</h2>
        <!-- <div style="margin-top: 10px;">{{ lang.domain_desc }}</div> -->
      </div>
      <div class="form narrow-form">
        <el-form label-width="120px">

          <el-form-item :label="lang.admin_account">
            <el-input v-bind:disabled="adminSettings.hadSeted" placeholder="请输入管理员用户名"
                      v-model="adminSettings.account"></el-input>
          </el-form-item>

          <el-form-item :label="lang.password">
            <el-input type="password" v-bind:disabled="adminSettings.hadSeted"
                      placeholder="请输入 8 到 12 个字节的密码"
                      v-model="adminSettings.password"
                      minlength="8"
                      maxlength="12"
                      autocomplete="new-password"
                      show-password></el-input>
            <div class="password-rule-tip">{{ lang.admin_pwd_desc }}</div>
          </el-form-item>

          <el-form-item :label="lang.enter_again">
            <el-input type="password" v-bind:disabled="adminSettings.hadSeted"
                      placeholder="请再次输入密码"
                      v-model="adminSettings.password2"
                      minlength="8"
                      maxlength="12"
                      autocomplete="new-password"
                      show-password></el-input>
          </el-form-item>
        </el-form>
      </div>
    </div>


    <div v-if="active === 3" class="ctn">
      <div class="desc">
        <h2>{{ lang.SetDomail }}</h2>
        <!-- <div style="margin-top: 10px;">{{ lang.domain_desc }}</div> -->
      </div>
      <div class="form narrow-form">
        <el-form label-width="120px">

          <el-form-item :label="lang.smtp_domain">
            <el-input placeholder="请输入 SMTP 域名" v-model="domainSettings.smtp_domain">
              <template #prepend>smtp.</template>
            </el-input>
          </el-form-item>

          <el-form-item :label="lang.web_domain">
            <el-input placeholder="请输入 Web 管理域名" v-model="domainSettings.web_domain"></el-input>
          </el-form-item>

          <el-form-item :label="lang.multi_domain_setting">
                        <span>{{ lang.multi_domain_setting_desc }}
                          <el-button @click="addDomain" size="small"
                                     type="success" :icon="Plus"
                                     circle>
                          </el-button>
                        </span>
            <el-input :placeholder="'请输入第 ' + (i + 1) + ' 个域名'" v-for="(item, i) in domainSettings.multi_domain "
                      v-model="domainSettings.multi_domain[i]" :key="item"></el-input>
          </el-form-item>


        </el-form>
      </div>
    </div>

    <div v-if="active === 4" class="dns-panel">
      <div class="dns-header">
        <div class="desc dns-heading">
          <div class="dns-eyebrow">{{ lang.dns_setup_step }}</div>
          <h2>{{ lang.setDNS }}</h2>
          <div class="dns-description">{{ lang.dns_desc }}</div>
        </div>
        <div class="dns-header-notes">
          <div class="dns-header-note">{{ lang.dns_record_format }}</div>
          <div class="dns-header-note dns-header-note-secondary">{{ lang.dns_ip_source }}</div>
          <div v-if="!hasDNSIPv6" class="dns-header-note dns-header-note-ipv6">
            {{ lang.dns_ipv6_unavailable }}
          </div>
        </div>
      </div>

      <div v-if="Object.keys(dnsInfos).length > 0" class="dns-domain-list">
        <section class="dns-domain-card" v-for="(info, domain) in dnsInfos" :key="domain">
          <div class="dns-domain-header">
            <div>
              <h3>{{ domain }}</h3>
              <span class="dns-domain-suffix">{{ lang.dns_domain_suffix }}</span>
            </div>
            <el-tag class="dns-domain-count" effect="plain" round>{{ info.length }} {{ lang.dns_record_count }}</el-tag>
          </div>

          <el-table :data="info" border class="dns-table" table-layout="auto">
            <el-table-column
              prop="host"
              :label="lang.dns_host"
              width="190px"
              align="center"
              header-align="center"
            >
              <template #default="scope">
                <el-tooltip :content="lang.dns_root_desc" placement="top" v-if="scope.row.host === '' || scope.row.host === '@'">
                  <span class="dns-host dns-host-root">{{ scope.row.host || '@' }}</span>
                </el-tooltip>
                <span v-else class="dns-host">{{ scope.row.host }}</span>
              </template>
            </el-table-column>

            <el-table-column
              prop="type"
              :label="lang.dns_type"
              width="110px"
              align="center"
              header-align="center"
            >
              <template #default="scope">
                <span :class="['dns-type', 'dns-type-' + scope.row.type.toLowerCase()]">
                  {{ scope.row.type }}
                </span>
              </template>
            </el-table-column>

            <el-table-column
              prop="value"
              :label="lang.dns_value"
              min-width="420px"
              header-align="center"
            >
              <template #default="scope">
                <div class="dns-value-cell">
                  <el-tooltip :content="scope.row.tips" placement="top" v-if="scope.row.tips">
                    <span :class="['dns-value', { 'dns-value-long': scope.row.value.length > 80 }]">
                      {{ scope.row.value }}
                    </span>
                  </el-tooltip>
                  <span v-else :class="['dns-value', { 'dns-value-long': scope.row.value.length > 80 }]">
                    {{ scope.row.value }}
                  </span>
                  <el-button class="dns-copy-button" link type="primary" @click.stop="copyDNSValue(scope.row.value)">
                    {{ lang.dns_copy }}
                  </el-button>
                </div>
              </template>
            </el-table-column>

            <el-table-column
              prop="ttl"
              :label="lang.dns_ttl"
              width="120px"
              align="center"
              header-align="center"
            >
              <template #default="scope">
                <span class="dns-ttl">{{ formatDNSRecordTTL(scope.row.ttl) }}</span>
              </template>
            </el-table-column>
          </el-table>
        </section>
      </div>

      <el-empty v-else :description="lang.dns_empty" />
    </div>

    <div v-if="active === 5" class="ctn setup-form-panel">
      <div class="desc">
        <div class="ssl-title-row">
          <h2>{{ lang.setSSL }}</h2>
          <div v-if="sslSettings.type === '0' && sslSettings.challenge === 'http' && port !== 80"
               class="ssl-port-warning" role="alert">
            <strong>{{ lang.warning }}</strong>
            <span>{{ lang.autoSSLWarn }}</span>
          </div>
        </div>
        <div class="ssl-title-description">{{ lang.setSSL }}</div>
      </div>
      <div class="form ssl-form">
        <el-form label-position="top">
          <el-form-item :label="lang.type">
            <el-select class="ssl-type-select" :placeholder="lang.ssl_auto" v-model="sslSettings.type" :disabled="dnsChecking">
              <el-option :label="lang.ssl_auto" value="0"/>
              <el-option :label="lang.ssl_manuallyf" value="1"/>
            </el-select>
          </el-form-item>

          <el-form-item :label="lang.ssl_challenge_type" v-if="sslSettings.type === '0'">
            <div class="ssl-challenge-select-row">
              <el-select class="ssl-type-select" :placeholder="lang.ssl_auto_http" v-model="sslSettings.challenge"
                         :disabled="dnsChecking || fullscreenLoading" @change="handleSSLChallengeChange">
                <el-option :label="lang.ssl_auto_http" value="http"/>
                <el-option :label="lang.ssl_auto_dns" value="dns"/>
              </el-select>

              <el-tooltip class="ssl-challenge-help" effect="dark" :content="lang.challenge_typ_desc"
                          placement="top-start">
                <span class="ssl-help-mark">?</span>
              </el-tooltip>
            </div>
          </el-form-item>

          <div v-if="sslSettings.type === '0'" class="ssl-storage-card">
            <div class="ssl-storage-title">{{ lang.ssl_auto_storage_title }}</div>
            <div class="ssl-storage-desc">{{ lang.ssl_auto_storage_desc }}</div>
            <div v-if="sslSettings.challenge === 'dns'" class="ssl-dns-intro">
              {{ lang.ssl_dns_desc }}
            </div>
            <div class="ssl-storage-path">{{ lang.ssl_auto_certificate_path }}</div>
            <div class="ssl-storage-path">{{ lang.ssl_auto_private_key_path }}</div>
            <div class="ssl-storage-path">{{ lang.ssl_auto_account_key_path }}</div>
          </div>


          <el-form-item :label="lang.ssl_key_path" v-if="sslSettings.type === '1'">
            <el-input placeholder="/ssl/example.com/example.com.key" v-model="sslSettings.key_path"></el-input>
            <div class="ssl-manual-desc">{{ lang.ssl_manual_key_desc }}</div>
          </el-form-item>

          <el-form-item :label="lang.ssl_crt_path" v-if="sslSettings.type === '1'">
            <el-input placeholder="/ssl/example.com/fullchain.pem" v-model="sslSettings.crt_path"></el-input>
            <div class="ssl-manual-desc">{{ lang.ssl_manual_crt_desc }}</div>
          </el-form-item>
        </el-form>


      </div>

    </div>

    <div v-if="dnsChecking" class="ssl-dns-challenge">
      <div class="ssl-dns-challenge-header">
        <div>
          <h3>{{ lang.ssl_dns_title }}</h3>
          <p v-if="sslSettings.paramsList.length === 0">{{ lang.ssl_dns_waiting }}</p>
          <p v-else>{{ lang.ssl_dns_ready }}</p>
        </div>
        <span v-if="sslSettings.paramsList.length > 0" class="ssl-dns-record-count">
          {{ sslSettings.paramsList.length }} 条记录
        </span>
      </div>
      <el-alert
        v-if="dnsChallengeNotice"
        :title="dnsChallengeNotice"
        type="warning"
        show-icon
        :closable="false"
        class="ssl-dns-refresh-notice"
      />
      <el-table v-if="sslSettings.paramsList.length > 0" :data="sslSettings.paramsList" border>
        <el-table-column prop="host" label="主机名" width="180px"/>
        <el-table-column prop="type" label="类型" width="100px"/>
        <el-table-column prop="value" label="记录值" min-width="360px">
          <template #default="scope">
            <div class="ssl-dns-value">{{ scope.row.value }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="ttl" label="TTL" width="100px"/>
      </el-table>
    </div>


    <div class="setup-actions">
      <el-button v-if="active > 0" id="previous" @click="previous">
        {{ lang.previous }}
      </el-button>
      <el-button :loading="databaseLoading || stepLoading" :disabled="databaseLoading || stepLoading || fullscreenLoading"
                 :element-loading-text="waitDesc"
                 v-loading.fullscreen.lock="fullscreenLoading" id="next"
                 @click="next">
           {{ dnsChecking ? lang.ssl_verify : lang.next }}
      </el-button>
    </div>

  </div>
</template>

<script setup>
import { usePage } from './Setup.js'

const {
  active,
  lang,
  dbSettings,
  databaseLoading,
  stepLoading,
  databaseError,
  adminSettings,
  domainSettings,
  addDomain,
  Plus,
  dnsInfos,
  hasDNSIPv6,
  sslSettings,
  port,
  dnsChecking,
  dnsChallengeNotice,
  waitDesc,
  next,
  previous,
  handleDbTypeChange,
  handleSSLChallengeChange,
  clearDatabaseError,
  formatDNSRecordTTL,
  copyDNSValue,
  fullscreenLoading
} = usePage()
</script>

<style scoped src="./Setup.css"></style>
