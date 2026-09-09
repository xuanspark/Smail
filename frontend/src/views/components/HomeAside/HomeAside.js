import { useRouter } from "vue-router";

import { ref, watch, computed } from "vue";

import useGroupStore from "../../../stores/group";

import lang from "../../../i18n/i18n";

import { http } from "@/utils/axios";

import { Setting } from "@element-plus/icons-vue";

import { useGlobalStatusStore } from "../../../stores/useGlobalStatusStore";

import BrandLogo from '../BrandLogo/BrandLogo.vue';
import { roleMeta } from '@/utils/userRoles.mjs';

export function usePage() {
  const groupStore = useGroupStore();
  
  const globalStatus = useGlobalStatusStore();
  
  const isLogin = computed(() => globalStatus.isLogin);

  const currentAccount = computed(() => globalStatus.userInfos.account || '账号未加载');

  const currentRole = computed(() => roleMeta(globalStatus.role));
  
  const router = useRouter();
  
  const data = ref([]);
  
  const searchQuery = ref("");
  
  const activeGroup = ref(groupStore.tag);
  
  watch(() => groupStore.tag, (newVal) => {
    activeGroup.value = newVal;
  });
  
  http.get("/api/v1/group").then((res) => {
    if (res.data) {
      // Attempting to flatten the tree for a simpler premium menu or keep it, el-tree is less beautiful.
      // For now we assume res.data is an array of items. If it's a tree, we flatten it.
      let list = [];
      const traverse = (items) => {
        items.forEach(node => {
          list.push(node);
          if(node.children) traverse(node.children);
        });
      }
      traverse(res.data);
      data.value = list;
    }
  });
  
  const handleMenuSelect = function (index) {
    const selected = data.value.find(d => d.tag === index);
    if (selected) {
      groupStore.name = selected.label;
      groupStore.tag = selected.tag;
      router.push({ name: "list" });
    }
  };
  
  const openSettings = function () {
    if (Object.keys(globalStatus.userInfos).length === 0) {
      globalStatus.init(() => {
        globalStatus.settingsDrawerVisible = true;
      });
    } else {
      globalStatus.settingsDrawerVisible = true;
    }
  };

  return {
    useRouter,
    ref,
    watch,
    computed,
    useGroupStore,
    lang,
    http,
    Setting,
    useGlobalStatusStore,
    BrandLogo,
    groupStore,
    globalStatus,
    isLogin,
    currentAccount,
    currentRole,
    router,
    data,
    searchQuery,
    activeGroup,
    handleMenuSelect,
    openSettings
  }
}

export {
  BrandLogo,
  Setting
}
