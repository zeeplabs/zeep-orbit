import { ClientConfig } from './types'
import { TableClient } from './table'
import { AuthClient } from './auth'
import { FilesClient } from './files'

export class OrbitClient {
  private config: ClientConfig
  auth: AuthClient
  files: FilesClient

  constructor(config: ClientConfig) {
    this.config = config
    this.auth = new AuthClient(config)
    this.files = new FilesClient(config)
  }

  table(name: string): TableClient {
    return new TableClient(this.config, name)
  }
}
