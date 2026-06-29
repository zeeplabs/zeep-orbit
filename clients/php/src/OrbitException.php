<?php

namespace Zeeplabs\Orbit;

class OrbitException extends \RuntimeException
{
    public function __construct(
        string $message,
        public readonly int $status = 0
    ) {
        parent::__construct($message);
    }
}
