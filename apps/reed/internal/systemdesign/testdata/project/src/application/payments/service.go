package payments

import "example.com/shop/src/infrastructure/storage"

func Pay() string { return storage.Save() }
