package orders

import "example.com/shop/src/application/payments"

func Submit() string { return payments.Pay() }
