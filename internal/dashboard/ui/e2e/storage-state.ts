import path from 'path'
import { fileURLToPath } from 'url'

const dirname = path.dirname(fileURLToPath(import.meta.url))

export const STORAGE_STATE_PATH = path.join(dirname, '.auth', 'state.json')
