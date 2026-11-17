import { actions } from './actions';
import { columnHelper } from './helper';
import { name } from './name';
import { type } from './type';
import { url } from './url';
import { resources } from './resources';

export const columns = [
  name,
  type,
  url,
  resources,
  columnHelper.accessor('GroupName', {
    header: 'Group Name',
  }),
  actions,
];
