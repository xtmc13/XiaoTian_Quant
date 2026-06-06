import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { DataTable, MemoizedTableRow } from '../DataTable'

describe('DataTable', () => {
  interface Item {
    id: string
    name: string
    value: number
  }

  const data: Item[] = [
    { id: '1', name: 'Alice', value: 100 },
    { id: '2', name: 'Bob', value: 200 },
    { id: '3', name: 'Charlie', value: 300 },
  ]

  const columns = [
    { key: 'name', title: 'Name', render: (item: Item) => item.name },
    { key: 'value', title: 'Value', render: (item: Item) => `$${item.value}` },
  ]

  it('renders column headers', () => {
    render(
      <DataTable
        data={data}
        columns={columns}
        keyExtractor={(item) => item.id}
      />
    )
    expect(screen.getByText('Name')).toBeTruthy()
    expect(screen.getByText('Value')).toBeTruthy()
  })

  it('renders all rows', () => {
    render(
      <DataTable
        data={data}
        columns={columns}
        keyExtractor={(item) => item.id}
      />
    )
    expect(screen.getByText('Alice')).toBeTruthy()
    expect(screen.getByText('Bob')).toBeTruthy()
    expect(screen.getByText('Charlie')).toBeTruthy()
    expect(screen.getByText('$100')).toBeTruthy()
    expect(screen.getByText('$200')).toBeTruthy()
    expect(screen.getByText('$300')).toBeTruthy()
  })

  it('renders empty state when no data', () => {
    render(
      <DataTable
        data={[]}
        columns={columns}
        keyExtractor={(item) => item.id}
        emptyText="No data available"
      />
    )
    expect(screen.getByText('No data available')).toBeTruthy()
  })

  it('uses custom empty text', () => {
    render(
      <DataTable
        data={[]}
        columns={columns}
        keyExtractor={(item) => item.id}
        emptyText="Custom empty message"
      />
    )
    expect(screen.getByText('Custom empty message')).toBeTruthy()
  })

  it('applies custom className', () => {
    const { container } = render(
      <DataTable
        data={data}
        columns={columns}
        keyExtractor={(item) => item.id}
        className="custom-table"
      />
    )
    expect(container.querySelector('.custom-table')).toBeTruthy()
  })
})

describe('MemoizedTableRow', () => {
  it('renders children', () => {
    render(
      <table>
        <tbody>
          <MemoizedTableRow>
            <td>Cell 1</td>
            <td>Cell 2</td>
          </MemoizedTableRow>
        </tbody>
      </table>
    )
    expect(screen.getByText('Cell 1')).toBeTruthy()
    expect(screen.getByText('Cell 2')).toBeTruthy()
  })

  it('calls onClick when clicked', () => {
    const handleClick = vi.fn()
    render(
      <table>
        <tbody>
          <MemoizedTableRow onClick={handleClick}>
            <td>Click me</td>
          </MemoizedTableRow>
        </tbody>
      </table>
    )
    screen.getByText('Click me').closest('tr')?.click()
    expect(handleClick).toHaveBeenCalledTimes(1)
  })
})
