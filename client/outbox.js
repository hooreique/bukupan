"use strict";

// A write is sent only after this local transaction commits. A server receipt is
// removed only after the server has committed, so reconnect/reload can retry it.
const BoardOutbox = {
  async open() {
    const db = await new Promise((resolve, reject) => {
      const request = indexedDB.open("bukupan", 1);
      request.onupgradeneeded = () => request.result.createObjectStore("pending", { keyPath: "id" });
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
      request.onblocked = () => reject(new Error("Close older tabs to open local storage"));
    });
    const transaction = (mode, action) => new Promise((resolve, reject) => {
      const tx = db.transaction("pending", mode);
      const request = action(tx.objectStore("pending"));
      tx.oncomplete = () => resolve(request.result);
      tx.onerror = tx.onabort = () => reject(tx.error || new Error("Local storage transaction failed"));
    });
    return {
      all: () => transaction("readonly", store => store.getAll()),
      put: record => transaction("readwrite", store => store.put(record)),
      remove: id => transaction("readwrite", store => store.delete(id)),
      close: () => db.close(),
    };
  },
};
