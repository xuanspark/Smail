import { reactive } from "vue";

import lang from "../../../i18n/i18n";

import { http } from "@/utils/axios";

import { ElMessage } from "element-plus";

import { Folder, Delete, Plus } from "@element-plus/icons-vue";

export function usePage() {
  const data = reactive([]);
  
  http.get("/api/group").then((res) => {
    data.push(...res.data);
  });
  
  const del = function (node, dataObj) {
    if (dataObj.id !== -1) {
      http.post("/api/group/del", { id: dataObj.id }).then((res) => {
        if (res.errorNo !== 0) {
          ElMessage({ message: res.errorMsg, type: "error" });
        } else {
          const pc = node.parent.childNodes;
          for (let i = 0; i < pc.length; i++) {
            if (pc[i].id === node.id) {
              pc.splice(i, 1);
              return;
            }
          }
        }
      });
    } else {
      const pc = node.parent.childNodes;
      for (let i = 0; i < pc.length; i++) {
        if (pc[i].id === node.id) {
          pc.splice(i, 1);
          return;
        }
      }
    }
  };
  
  const add = function (item) {
    if (item.children == null) {
      item.children = [];
    }
    item.children.push({
      children: [],
      label: "",
      id: -1,
      parent_id: item.id,
    });
  };
  
  const addRoot = function () {
    data.push({
      children: [],
      label: "",
      id: -1,
      parent_id: 0,
    });
  };
  
  const onInputBlur = function (item) {
    if (item.label !== "") {
      http.post("/api/group/add", { name: item.label, parent_id: item.parent_id })
        .then((res) => {
          if (res.errorNo !== 0) {
            ElMessage({ message: res.errorMsg, type: "error" });
          } else {
            http.get("/api/group").then((res) => {
              data.splice(0, data.length);
              data.push(...res.data);
            });
          }
        });
    }
  };

  return {
    reactive,
    lang,
    http,
    ElMessage,
    Folder,
    Delete,
    Plus,
    data,
    del,
    add,
    addRoot,
    onInputBlur
  }
}

export {
  Folder,
  Plus,
  Delete
}
