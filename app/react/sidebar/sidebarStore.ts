import create from 'zustand';

import {
  get as getFromStorage,
  set as setToStorage,
  keyBuilder,
} from '@/react/hooks/useLocalStorage';
import { getAppearance } from '@/react/portainer/appearance/appearance';

const storageKey = 'toolbar_toggle';
const mobileWidth = 992;

function getInitialIsOpen() {
  if (window.ddExtension) return false;
  if (window.innerWidth < mobileWidth) return false;
  return getFromStorage<boolean>(storageKey, getAppearance().Layout.SidebarDefault === 'open');
}

interface SidebarStore {
  isOpen: boolean;
  toggle: () => void;
  setOpen: (value: boolean) => void;
}

export const sidebarStore = create<SidebarStore>()((set, get) => ({
  isOpen: getInitialIsOpen(),
  toggle: () => {
    const newIsOpen = !get().isOpen;
    setToStorage(storageKey, newIsOpen);
    set({ isOpen: newIsOpen });
  },
  setOpen: (value: boolean) => {
    set({ isOpen: value });
  },
}));

export function applyDefaultSidebarState() {
  if (window.ddExtension || window.innerWidth < mobileWidth || localStorage.getItem(keyBuilder(storageKey)) !== null) return;
  sidebarStore.getState().setOpen(getAppearance().Layout.SidebarDefault === 'open');
}
