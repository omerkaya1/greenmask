package transformers

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/greenmaskio/greenmask/internal/generators"
)

const uuidTransformerRequiredLength = 16

type UuidTransformer struct {
	byteLength int
	generator  generators.Generator
}

func NewUuidTransformer() *UuidTransformer {
	return &UuidTransformer{
		byteLength: uuidTransformerRequiredLength,
	}
}

func (ut *UuidTransformer) Transform(data []byte) (uuid.UUID, error) {
	resBytes, err := ut.generator.Generate(data)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return uuid.FromBytes(resBytes)
}

func (ut *UuidTransformer) GetRequiredGeneratorByteLength() int {
	return ut.byteLength
}

func (ut *UuidTransformer) SetGenerator(g generators.Generator) error {
	if g.Size() < ut.byteLength {
		return fmt.Errorf("requested byte length (%d) higher than generator can produce (%d)", ut.byteLength, g.Size())
	}
	ut.generator = g
	return nil
}
