<template>
<div id="main">
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

    <div v-if="active === 1" class="ctn">
      <div class="desc">
        <h2>{{ lang.select_db }}</h2>
        <div style="margin-top: 10px;">{{ lang.db_desc }}</div>
      </div>
      <div class="form" style="width: 400px;">
        <el-form label-width="120px">
          <el-form-item :label="lang.type">
            <el-select :placeholder="lang.db_select_ph" v-model="dbSettings.type"
                       @change="dbSettings.dsn = ''">
              <el-option label="MySQL" value="mysql"/>
              <el-option label="SQLite3" value="sqlite"/>
              <el-option label="PostgreSQL" value="postgres"/>
            </el-select>
          </el-form-item>

          <el-form-item :label="lang.mysql_dsn" v-if="dbSettings.type === 'mysql'">
            <el-input :rows="2" type="textarea" v-model="dbSettings.dsn"
                      placeholder="请输入 MySQL 数据库连接信息"></el-input>
          </el-form-item>

          <el-form-item :label="lang.pg_dsn" v-if="dbSettings.type === 'postgres'">
            <el-input :rows="2" type="textarea" v-model="dbSettings.dsn"
                      placeholder="请输入 PostgreSQL 数据库连接信息"></el-input>
          </el-form-item>

          <el-form-item :label="lang.sqlite_db_path" v-if="dbSettings.type === 'sqlite'">
            <el-input v-model="dbSettings.dsn" placeholder="请输入 SQLite 数据文件路径"></el-input>
          </el-form-item>
        </el-form>
      </div>
    </div>


    <div v-if="active === 2" class="ctn">
      <div class="desc">
        <h2>{{ lang.setAdminPassword }}</h2>
        <!-- <div style="margin-top: 10px;">{{ lang.domain_desc }}</div> -->
      </div>
      <div class="form" style="width: 400px;">
        <el-form label-width="120px">

          <el-form-item :label="lang.admin_account">
            <el-input v-bind:disabled="adminSettings.hadSeted" placeholder="请输入管理员用户名"
                      v-model="adminSettings.account"></el-input>
          </el-form-item>

          <el-form-item :label="lang.password">
            <el-input type="password" v-bind:disabled="adminSettings.hadSeted" placeholder=""
                      v-model="adminSettings.password"></el-input>
          </el-form-item>

          <el-form-item :label="lang.enter_again">
            <el-input type="password" v-bind:disabled="adminSettings.hadSeted" placeholder=""
                      v-model="adminSettings.password2"></el-input>
          </el-form-item>
        </el-form>
      </div>
    </div>


    <div v-if="active === 3" class="ctn">
      <div class="desc">
        <h2>{{ lang.SetDomail }}</h2>
        <!-- <div style="margin-top: 10px;">{{ lang.domain_desc }}</div> -->
      </div>
      <div class="form" style="width: 400px;">
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

    <div v-if="active === 4" class="ctn_s">

      <div class="desc">
        <h2>{{ lang.setDNS }}</h2>
        <div style="margin-top: 10px;">{{ lang.dns_desc }}</div>
      </div>
      <div class="form" width="600px" v-for="(info,domain) in dnsInfos" :key="info">
        <h3>{{ domain }}</h3>
        <el-table :data="info" border style="width: 100%">
          <el-table-column prop="host" label="主机名" width="110px">
            <template #default="scope">
              <div style="display: flex; align-items: center">
                <el-tooltip :content="lang.dns_root_desc" placement="top"
                            v-if="scope.row.host === '' || scope.row.host === '@' ">
                  {{ scope.row.host }}
                </el-tooltip>
                <span v-else>{{ scope.row.host }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column prop="type" label="类型" width="110px"/>
          <el-table-column prop="value" label="值">
            <template #default="scope">
              <div style="display: flex; align-items: center">
                <el-tooltip :content="scope.row.tips" placement="top" v-if="scope.row.tips !== ''">
                  {{ scope.row.value }}
                </el-tooltip>
                <span v-else>{{ scope.row.value }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column prop="ttl" label="TTL" width="110px"/>
        </el-table>
      </div>
    </div>

    <el-alert :closable="false" title="警告" type="error" center
              v-if="active === 5 && sslSettings.type === 0 && port !== 80" :description="lang.autoSSLWarn"/>

    <div v-if="active === 5" class="ctn">
      <div class="desc">
        <h2>{{ lang.setSSL }}</h2>
        <div style="margin-top: 10px;">{{ lang.setSSL }}</div>
      </div>
      <div class="form" width="600px">
        <el-form label-width="120px">
          <el-form-item :label="lang.type">
            <el-select :placeholder="lang.ssl_auto" v-model="sslSettings.type" :disabled="dnsChecking">
              <el-option :label="lang.ssl_auto" value="0"/>
              <el-option :label="lang.ssl_manuallyf" value="1"/>
            </el-select>
          </el-form-item>

          <el-form-item :label="lang.ssl_challenge_type" v-if="sslSettings.type === '0'">
            <el-select :placeholder="lang.ssl_auto_http" v-model="sslSettings.challenge"
                       :disabled="dnsChecking">
              <el-option :label="lang.ssl_auto_http" value="http"/>
              <el-option :label="lang.ssl_auto_dns" value="dns"/>
            </el-select>

            <el-tooltip class="box-item" effect="dark" :content="lang.challenge_typ_desc"
                        placement="top-start">
              <span style="margin-left: 6px; font-size:18px; font-weight: bolder;">?</span>
            </el-tooltip>
          </el-form-item>


          <el-form-item :label="lang.ssl_key_path" v-if="sslSettings.type === '1'">
            <el-input placeholder="./config/ssl/private.key" v-model="sslSettings.key_path"></el-input>
          </el-form-item>

          <el-form-item :label="lang.ssl_crt_path" v-if="sslSettings.type === '1'">
            <el-input placeholder="./config/ssl/public.crt" v-model="sslSettings.crt_path"></el-input>
          </el-form-item>
        </el-form>


      </div>

    </div>

    <div v-if="dnsChecking">
      <label>{{ lang.dns_desc }}</label>
      <el-table :data="sslSettings.paramsList" border v-loading="sslSettings.paramsList.length === 0">
        <el-table-column prop="host" label="主机名" width="110px"/>
        <el-table-column prop="type" label="类型" width="110px"/>
        <el-table-column prop="value" label="值">
          <template #default="scope">
            <div style="display: flex; align-items: center">
              <el-tooltip :content="scope.row.tips" placement="top" v-if="scope.row.tips !== ''">
                {{ scope.row.value }}
              </el-tooltip>
              <span v-else>{{ scope.row.value }}</span>
            </div>
          </template>

        </el-table-column>
        <el-table-column prop="ttl" label="TTL" width="110px"/>
      </el-table>

    </div>


    <el-button :element-loading-text="waitDesc" v-loading.fullscreen.lock="fullscreenLoading" id="next"
               style="margin-top: 12px" @click="next">{{
        lang.next
      }}
    </el-button>

  </div>
</template>

<script setup>
import { usePage } from './Setup.js'

const {
  active,
  lang,
  dbSettings,
  adminSettings,
  domainSettings,
  addDomain,
  Plus,
  dnsInfos,
  sslSettings,
  port,
  dnsChecking,
  waitDesc,
  next,
  fullscreenLoading
} = usePage()
</script>

<style scoped src="./Setup.css"></style>
