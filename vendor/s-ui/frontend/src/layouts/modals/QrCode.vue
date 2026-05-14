<template>
  <v-dialog transition="dialog-bottom-transition" width="400">
    <v-card class="rounded-lg" id="qrcode-modal" :loading="loading">
      <v-card-title>
        <v-row>
          <v-col>QrCode</v-col>
          <v-spacer></v-spacer>
          <v-col cols="auto"><v-icon icon="mdi-close-box" @click="$emit('close')" /></v-col>
        </v-row>
      </v-card-title>
      <v-divider></v-divider>
      <v-skeleton-loader
          class="mx-auto border"
          width="80%"
          type="text, image, divider, text, image"
          v-if="loading"
        ></v-skeleton-loader>
      <v-card-text style="overflow-y: auto; padding: 0" :hidden="loading">
        <v-tabs
          v-model="tab"
          density="compact"
          fixed-tabs
          align-tabs="center"
        >
          <v-tab value="sub">{{ $t('setting.sub') }}</v-tab>
          <v-tab value="link">{{ $t('client.links') }}</v-tab>
          <v-tab value="files">{{ $t('client.files') }}</v-tab>
        </v-tabs>
        <v-window v-model="tab" style="margin-top: 10px;">
          <v-window-item value="sub">
            <v-row>
              <v-col style="text-align: center;">
                <v-chip>{{ $t('setting.jsonSubL3') }}</v-chip><br />
                <QrcodeVue :value="clientSub + '?format=json-l3router'" :size="size" @click="copyToClipboard(clientSub + '?format=json-l3router')" :margin="1" style="border-radius: 1rem; cursor: copy;" />
              </v-col>
            </v-row>
            <v-row>
              <v-col style="text-align: center;">
                <v-chip>{{ $t('setting.jsonSubWG') }}</v-chip><br />
                <QrcodeVue :value="clientSub + '?format=json-wg'" :size="size" @click="copyToClipboard(clientSub + '?format=json-wg')" :margin="1" style="border-radius: 1rem; cursor: copy;" />
              </v-col>
            </v-row>
            <v-row>
              <v-col style="text-align: center;">
                <v-chip>{{ $t('setting.jsonSubMasque') }}</v-chip><br />
                <QrcodeVue :value="clientSub + '?format=json-masque'" :size="size" @click="copyToClipboard(clientSub + '?format=json-masque')" :margin="1" style="border-radius: 1rem; cursor: copy;" />
              </v-col>
            </v-row>
            <v-row>
              <v-col style="text-align: center;">
                <v-chip>{{ $t('setting.jsonSubRule') }}</v-chip><br />
                <QrcodeVue :value="clientSub + '?format=json-rule'" :size="size" @click="copyToClipboard(clientSub + '?format=json-rule')" :margin="1" style="border-radius: 1rem; cursor: copy;" />
              </v-col>
            </v-row>
            <v-row>
              <v-col style="text-align: center;">
                <v-chip>{{ $t('setting.jsonSubHapp') }}</v-chip><br />
                <QrcodeVue :value="clientSub + '?format=happ'" :size="size" @click="copyToClipboard(clientSub + '?format=happ')" :margin="1" style="border-radius: 1rem; cursor: copy;" />
              </v-col>
            </v-row>
            <v-row>
              <v-col style="text-align: center;">
                <v-chip>{{ $t('setting.sub') }}</v-chip><br />
                <QrcodeVue :value="clientSub" :size="size" @click="copyToClipboard(clientSub)" :margin="1" style="border-radius: 1rem; cursor: copy;" />
              </v-col>
            </v-row>
            <v-row>
              <v-col style="text-align: center;">
                <v-chip>{{ $t('setting.jsonSub') }}</v-chip><br />
                <QrcodeVue :value="clientSub + '?format=json'" :size="size" @click="copyToClipboard(clientSub + '?format=json')" :margin="1" style="border-radius: 1rem; cursor: copy;" />
              </v-col>
            </v-row>
            <v-row>
              <v-col style="text-align: center;">
                <v-chip>{{ $t('setting.clashSub') }}</v-chip><br />
                <QrcodeVue :value="clientSub + '?format=clash'" :size="size" @click="copyToClipboard(clientSub + '?format=clash')" :margin="1" style="border-radius: 1rem; cursor: copy;" />
              </v-col>
            </v-row>
            <v-row>
              <v-col style="text-align: center;">
                <v-chip>SING-BOX (scan only)</v-chip><br />
                <QrcodeVue :value="singboxRule" :size="size" :margin="1" style="border-radius: .8rem; cursor: not-allowed;" />
              </v-col>
            </v-row>
          </v-window-item>
          <v-window-item value="link">
            <v-row v-if="ruleLinks.length > 0">
              <v-col style="text-align: center;">
                <v-chip color="secondary">{{ $t('client.rules') }}</v-chip>
              </v-col>
            </v-row>
            <v-row v-for="r in ruleLinks">
              <v-col style="text-align: center;">
                <v-chip>{{ r.remark ?? $t('client.rules') }}</v-chip><br />
                <QrcodeVue :value="r.uri" :size="size" @click="copyToClipboard(r.uri)" :margin="1" style="border-radius: .5rem; cursor: copy;" />
              </v-col>
            </v-row>
            <v-row v-if="ruleLinks.length > 0">
              <v-col style="text-align: center;">
                <v-divider />
              </v-col>
            </v-row>
            <v-row v-for="l in clientLinks">
              <v-col style="text-align: center;">
                <v-chip>{{ l.remark?? $t('client.' + l.type) }}</v-chip><br />
                <QrcodeVue :value="l.uri" :size="size" @click="copyToClipboard(l.uri)" :margin="1" style="border-radius: .5rem; cursor: copy;" />
              </v-col>
            </v-row>
          </v-window-item>
          <v-window-item value="files">
            <v-row v-if="allFiles.length === 0">
              <v-col style="text-align: center;">
                <v-chip>{{ $t('client.noFiles') }}</v-chip>
              </v-col>
            </v-row>
            <v-list v-else density="compact">
              <v-list-item v-for="f in allFiles" :key="`${f.family}-${f.endpoint_id}`">
                <template #title>
                  <span>{{ f.endpoint_tag }}</span>
                </template>
                <template #subtitle>
                  <span>{{ f.family === 'awg' ? 'AWG' : 'WG' }}</span>
                </template>
                <template #append>
                  <v-btn size="small" variant="outlined" @click="downloadConfigFile(f)">
                    {{ $t('client.downloadConf') }}
                  </v-btn>
                </template>
              </v-list-item>
            </v-list>
          </v-window-item>
        </v-window>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import QrcodeVue from 'qrcode.vue'
import Data from '@/store/modules/data'
import Clipboard from 'clipboard'
import { i18n } from '@/locales'
import { push } from 'notivue'
import HttpUtils from '@/plugins/httputil'

export default {
  props: ['id', 'visible'],
  data() {
    return {
      tab: "sub",
      client: <any>{},
      awgFiles: <any[]>[],
      wgFiles: <any[]>[],
      ruleLinks: <any[]>[],
      loading: false,
    }
  },
  methods: {
    async load() {
      this.loading = true
      const newData = await Data().loadClients(this.$props.id)
      this.client = newData
      const files = await HttpUtils.get(`api/client/${this.$props.id}/files/awg-conf`)
      this.awgFiles = files.success ? (files.obj?.files ?? []) : []
      const wgFiles = await HttpUtils.get(`api/client/${this.$props.id}/files/wg-conf`)
      this.wgFiles = wgFiles.success ? (wgFiles.obj?.files ?? []) : []
      const rules = await HttpUtils.get(`api/client/${this.$props.id}/rules-links`)
      this.ruleLinks = rules.success ? (rules.obj?.rules_links ?? []) : []
      this.loading = false
    },
    downloadConfigFile(file: any) {
      if (!file?.download_url) return
      const link = document.createElement('a')
      link.href = file.download_url
      link.rel = 'noopener'
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
    },
    copyToClipboard(txt:string) {
      const hiddenButton = document.createElement('button')
      hiddenButton.className = 'clipboard-btn'
      document.body.appendChild(hiddenButton)

      const clipboard = new Clipboard('.clipboard-btn', {
        text: () => txt,
        container: document.getElementById('qrcode-modal')?? undefined
      });

      clipboard.on('success', () => {
        clipboard.destroy()
        push.success({
          message: i18n.global.t('success') + ": " + i18n.global.t('copyToClipboard'),
          duration: 5000,
        })
      })

      clipboard.on('error', () => {
        clipboard.destroy()
        push.error({
          message: i18n.global.t('failed') + ": " + i18n.global.t('copyToClipboard'),
          duration: 5000,
        })
      })

      // Perform click on hidden button to trigger copy
      hiddenButton.click()
      document.body.removeChild(hiddenButton)
    }
  },
  computed: {
    clientSub() {
      return Data().subURI + this.client.name
    },
    singbox() {
      const url = Data().subURI + this.client.name + "?format=json"
      return "sing-box://import-remote-profile?url=" +  encodeURIComponent(url) + "#" + this.client.name
    },
    singboxRule() {
      const url = Data().subURI + this.client.name + "?format=json-rule"
      return "sing-box://import-remote-profile?url=" +  encodeURIComponent(url) + "#" + this.client.name + "-rule"
    },
    clientLinks() {
      return this.client.links?? []
    },
    allFiles() {
      const awg = (this.awgFiles ?? []).map((f: any) => ({ ...f, family: 'awg' }))
      const wg = (this.wgFiles ?? []).map((f: any) => ({ ...f, family: 'wg' }))
      return [...awg, ...wg]
    },
    size() {
      if (window.innerWidth > 380) return 300
      if (window.innerWidth > 330) return 280
      return 250
    }
  },
  watch: {
    visible(v) {
      if (v) {
        this.tab = "sub"
        this.load()
      }
    },
  },
  components: { QrcodeVue }
}
</script>